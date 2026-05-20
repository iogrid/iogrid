/**
 * Thin REST client for the iogrid gateway-bff.
 *
 * Why a hand-rolled fetch wrapper instead of generated Connect clients:
 *  - The BFF speaks plain JSON (no protobuf at the edge); the generated
 *    classes pull in @bufbuild/protobuf at runtime which doubles the
 *    client bundle.
 *  - We want the same module to work in both Server Components (where
 *    we can read the NextAuth session and forward the access token) and
 *    Client Components (where the token lives in a cookie set by an
 *    upstream proxy or — for API-key callers — in localStorage).
 *
 * Auth precedence in the browser:
 *   1. explicit `token` option
 *   2. iogrid_api_key in localStorage (customer dashboards)
 *   3. nothing — the gateway falls through to its anonymous limiter.
 */

// Default to same-origin so the browser sends the NextAuth cookie on
// every /api/v1/* fetch and the Next.js Route Handlers at the same
// origin can read the session + bridge to gateway-bff via the
// service-token shim (issue #237).
//
// The two NEXT_PUBLIC_* env vars are still honoured for tests and for
// the rare caller that wants to hit a *different* origin directly
// (e.g. health checks against a remote API). Leave them unset in
// production to keep all /api/v1/* same-origin.
const DEFAULT_BASE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_URL ??
  process.env.NEXT_PUBLIC_API_BASE_URL ??
  "";

export interface ApiClientOptions {
  baseUrl?: string;
  token?: string;
  /**
   * Override fetch — used by tests to avoid hitting the network.
   */
  fetcher?: typeof fetch;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export class ApiClient {
  readonly baseUrl: string;
  private readonly token?: string;
  private readonly fetcher: typeof fetch;

  constructor(opts: ApiClientOptions = {}) {
    this.baseUrl = (opts.baseUrl ?? DEFAULT_BASE_URL).replace(/\/$/, "");
    this.token = opts.token;
    // BUG: assigning `fetch` to an instance property loses its `this`
    // binding to `Window` / `WorkerGlobalScope`. Calling
    // `this.fetcher(url, opts)` then throws
    //   TypeError: Failed to execute 'fetch' on 'Window': Illegal invocation
    // Fix: bind to globalThis so the runtime can call it stand-alone.
    this.fetcher = opts.fetcher ?? fetch.bind(globalThis);
  }

  private headers(extra?: HeadersInit): HeadersInit {
    const h: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    const tok = this.resolveToken();
    if (tok) h["Authorization"] = `Bearer ${tok}`;
    return { ...h, ...(extra as Record<string, string>) };
  }

  private resolveToken(): string | undefined {
    if (this.token) return this.token;
    if (typeof window !== "undefined") {
      try {
        return window.localStorage.getItem("iogrid_api_key") ?? undefined;
      } catch {
        return undefined;
      }
    }
    return undefined;
  }

  async get<T>(path: string, init?: RequestInit): Promise<T> {
    return this.request<T>("GET", path, undefined, init);
  }

  async post<T>(path: string, body?: unknown, init?: RequestInit): Promise<T> {
    return this.request<T>("POST", path, body, init);
  }

  /**
   * PUT for idempotent replacement semantics. Used by the payout-method
   * surface (#324) where the underlying storage is keyed by user_id and
   * a second call overwrites the first.
   */
  async put<T>(path: string, body?: unknown, init?: RequestInit): Promise<T> {
    return this.request<T>("PUT", path, body, init);
  }

  async del<T>(path: string, init?: RequestInit): Promise<T> {
    return this.request<T>("DELETE", path, undefined, init);
  }

  /**
   * DELETE with a JSON body. The HTTP spec permits a body on DELETE and
   * the iogrid identity-svc DeleteAccount RPC uses it to carry the
   * optional `reason` + `step_up_token` fields.
   */
  async delWithBody<T>(
    path: string,
    body: unknown,
    init?: RequestInit,
  ): Promise<T> {
    return this.request<T>("DELETE", path, body, init);
  }

  private async request<T>(
    method: string,
    path: string,
    body: unknown,
    init?: RequestInit,
  ): Promise<T> {
    const url = `${this.baseUrl}${path.startsWith("/") ? path : `/${path}`}`;
    const res = await this.fetcher(url, {
      ...init,
      method,
      headers: this.headers(init?.headers),
      body: body === undefined ? undefined : JSON.stringify(body),
      // Server-Components fetches must be uncached for live data —
      // callers can override per-request via init.
      cache: init?.cache ?? "no-store",
    });
    if (res.status === 204) {
      return undefined as T;
    }
    const text = await res.text();
    let parsed: unknown = undefined;
    if (text) {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = text;
      }
    }
    if (!res.ok) {
      const e = parsed as { code?: string; message?: string } | undefined;
      // Phase 0 backends deliberately return HTTP 501 +
      // `{code:"unimplemented"}` for RPCs that have no concrete binding
      // yet (see gateway-bff writeUpstreamError → CodeUnimplemented).
      // The operator-facing surface is supposed to render its existing
      // empty-state in that case, NOT a red "Couldn't load X" banner —
      // the feature is not broken, it's just not built. Translate the
      // 501 into an empty object so `data?.rows ?? []` / `data?.state`
      // accesses naturally fall through to the empty path. Genuine
      // failures (500 / 502 / 503 / 504 / 4xx) still throw. (#300)
      if (res.status === 501 && e?.code === "unimplemented") {
        return {} as T;
      }
      throw new ApiError(
        res.status,
        e?.code ?? "http_error",
        e?.message ?? res.statusText,
      );
    }
    return parsed as T;
  }
}

/**
 * Per-request helper: build an ApiClient that picks up the NextAuth
 * session token from cookies (server-side). Falls back to anon.
 */
export async function serverApi(token?: string): Promise<ApiClient> {
  return new ApiClient({ token });
}

/**
 * Browser-side helper: returns a memoised default client. Components
 * should call this rather than `new ApiClient()` directly so future
 * cross-cutting middleware (telemetry, retry) lands in one place.
 */
let _browserClient: ApiClient | undefined;
export function browserApi(): ApiClient {
  if (typeof window === "undefined") {
    return new ApiClient();
  }
  if (!_browserClient) _browserClient = new ApiClient();
  return _browserClient;
}
