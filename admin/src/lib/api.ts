/**
 * Thin REST client for the iogrid gateway-bff, scoped to the admin app.
 *
 * Mirrors `web/src/lib/api.ts` but slimmer — the admin surface never
 * uses API-key auth (no customer-dashboard localStorage), only the
 * same-origin NextAuth cookie. Same precedence rules so future cross-
 * cutting middleware (telemetry, retry) lands in one place.
 */

const DEFAULT_BASE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_URL ??
  process.env.NEXT_PUBLIC_API_BASE_URL ??
  "";

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export interface ApiClientOptions {
  baseUrl?: string;
  token?: string;
  fetcher?: typeof fetch;
}

export class ApiClient {
  readonly baseUrl: string;
  private readonly token?: string;
  private readonly fetcher: typeof fetch;

  constructor(opts: ApiClientOptions = {}) {
    this.baseUrl = (opts.baseUrl ?? DEFAULT_BASE_URL).replace(/\/$/, "");
    this.token = opts.token;
    // Bind to globalThis so calling `this.fetcher(url, opts)` doesn't
    // throw the "Illegal invocation" TypeError on Window.
    this.fetcher = opts.fetcher ?? fetch.bind(globalThis);
  }

  private headers(extra?: HeadersInit): HeadersInit {
    const h: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    if (this.token) h["Authorization"] = `Bearer ${this.token}`;
    return { ...h, ...(extra as Record<string, string>) };
  }

  async get<T>(path: string, init?: RequestInit): Promise<T> {
    return this.request<T>("GET", path, undefined, init);
  }

  async post<T>(path: string, body?: unknown, init?: RequestInit): Promise<T> {
    return this.request<T>("POST", path, body, init);
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
      // Phase 0 backends return HTTP 501 + {code:"unimplemented"} for
      // RPCs with no concrete binding yet. Translate to empty object so
      // the empty-state branches in components naturally engage. See
      // gateway-bff's writeUpstreamError → CodeUnimplemented (#300).
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

let _browserClient: ApiClient | undefined;
export function browserApi(): ApiClient {
  if (typeof window === "undefined") {
    return new ApiClient();
  }
  if (!_browserClient) _browserClient = new ApiClient();
  return _browserClient;
}
