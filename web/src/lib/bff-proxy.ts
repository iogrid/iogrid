/**
 * bff-proxy — same-origin Next.js Route Handler → gateway-bff bridge.
 *
 * Why this exists (issue #237):
 *   Browser fetches from `app.iogrid.org` to `api.iogrid.org/api/v1/*`
 *   were returning HTTP 401 because the web uses NextAuth (cookies)
 *   and gateway-bff requires an identity-svc Bearer JWT — no bridge.
 *
 *   Approach A from the ticket: mount Next.js Route Handlers under
 *   `/api/v1/*` (same-origin, so the browser sends the NextAuth cookie),
 *   read the NextAuth session server-side, and forward to gateway-bff
 *   with the shared IOGRID_SERVICE_TOKEN + X-Iogrid-User-Id shim
 *   (same pattern as PR #233's `/api/customer/workspaces/init`).
 *
 *   The shared service token is mounted from a sealed Secret into BOTH
 *   the `web` Deployment and `gateway-bff` Deployment. See
 *   `docs/PHASE0-UNBLOCK.md` step 4c.
 *
 * Auth model:
 *   - The Next.js BFF reads the NextAuth cookie and trusts it.
 *   - It calls gateway-bff with Authorization: Bearer <service-token>
 *     plus X-Iogrid-User-Id, X-Iogrid-User-Roles, X-Iogrid-User-Email.
 *     gateway-bff's auth middleware short-circuits to a materialised
 *     Claims object on that combination — see
 *     coordinator/services/gateway-bff/internal/auth/auth.go.
 *   - When IOGRID_GATEWAY_BFF_URL or IOGRID_SERVICE_TOKEN is absent
 *     the proxy returns 503 (caller treats as "BFF not reachable").
 *
 * Streaming: SSE pass-through is supported via the `stream: true`
 * option. The response is returned with its original headers (incl.
 * `content-type: text/event-stream`) so EventSource consumers see
 * the real frames; the chunk stream is forwarded straight through
 * Next.js's edge-incompatible Response constructor.
 *
 * End state: once NextAuth → identity-svc token exchange ships, every
 * caller of this helper can be removed and the browser can call
 * gateway-bff directly with its own JWT. Until then, this is the
 * canonical web → BFF bridge.
 */
import { NextRequest, NextResponse } from "next/server";

import { auth } from "@/lib/auth";

/** Options for a single proxy call. */
export interface ProxyToBffOptions {
  /**
   * Pass-through mode. When true the upstream response is returned
   * verbatim (status + headers + streaming body). Use for SSE.
   * When false (the default) the body is buffered and returned as
   * a NextResponse with JSON content-type preserved.
   */
  stream?: boolean;
  /**
   * Additional comma-separable roles to set in X-Iogrid-User-Roles.
   * Useful when the route handler knows the path requires a specific
   * role and wants gateway-bff's RequireRole(...) to accept the
   * materialised Claims. Roles already present in the NextAuth
   * session are merged with these.
   *
   * Note: admin surfaces (/api/v1/admin/*) are NOT proxied through
   * web/ — they live in the separate admin/ Next.js app, which has
   * its own bff-proxy + session. See PR #425 / EPIC #422.
   */
  extraRoles?: string[];
  /**
   * Override the request method. Defaults to the incoming method.
   */
  method?: string;
  /**
   * Override the upstream path. Defaults to the request URL's
   * pathname + search.
   */
  upstreamPath?: string;
}

/**
 * `proxyToBff` is the workhorse: reads the session, forwards the
 * request to gateway-bff, and returns the response. Centralised here
 * so every Route Handler under `/api/v1/*` stays ~10 lines.
 */
export async function proxyToBff(
  req: NextRequest,
  opts: ProxyToBffOptions = {},
): Promise<Response> {
  const session = await auth();
  if (!session?.user) {
    return NextResponse.json(
      { code: "unauthenticated", message: "sign in first" },
      { status: 401 },
    );
  }
  const userId = (session.user as { id?: string }).id ?? "";
  if (!userId) {
    return NextResponse.json(
      { code: "no_user_id", message: "session is missing user id" },
      { status: 500 },
    );
  }

  const upstreamBase =
    process.env.IOGRID_GATEWAY_BFF_URL ??
    "http://gateway-bff.iogrid.svc.cluster.local:8080";
  const serviceToken = process.env.IOGRID_SERVICE_TOKEN ?? "";

  if (!upstreamBase || !serviceToken) {
    return NextResponse.json(
      {
        code: "bff_proxy_unavailable",
        message:
          "gateway-bff proxy not configured (missing IOGRID_GATEWAY_BFF_URL or IOGRID_SERVICE_TOKEN env)",
      },
      { status: 503 },
    );
  }

  const reqUrl = new URL(req.url);
  // `upstreamPath` overrides the URL path entirely (used by /admin/
  // providers/list which fans Connect-RPC). When omitted, the
  // same-origin path is forwarded verbatim. Search params (provider_id,
  // start, end, ...) are kept intact.
  const path = opts.upstreamPath ?? reqUrl.pathname + reqUrl.search;
  const upstreamURL = trimSlash(upstreamBase) + path;

  // Build the outbound headers. Session is the source of truth for
  // user_id + email; roles are session.role(s) ∪ extraRoles (caller-
  // supplied for /admin/*).
  const session_user = session.user as {
    id?: string;
    email?: string | null;
    roles?: string[];
    role?: string;
  };
  const sessionRoles: string[] = Array.isArray(session_user.roles)
    ? session_user.roles
    : session_user.role
      ? [session_user.role]
      : [];
  const roles = Array.from(
    new Set([...sessionRoles, ...(opts.extraRoles ?? [])]),
  );

  const outHeaders: Record<string, string> = {
    authorization: `Bearer ${serviceToken}`,
    "x-iogrid-user-id": userId,
  };
  if (session_user.email) {
    outHeaders["x-iogrid-user-email"] = session_user.email;
  }
  if (roles.length > 0) {
    outHeaders["x-iogrid-user-roles"] = roles.join(",");
  }

  // Forward content-type + accept verbatim when present so JSON vs
  // SSE negotiation survives the hop.
  const contentType = req.headers.get("content-type");
  if (contentType) outHeaders["content-type"] = contentType;
  const accept = req.headers.get("accept");
  if (accept) outHeaders["accept"] = accept;

  const method = opts.method ?? req.method;
  let body: BodyInit | undefined;
  if (method !== "GET" && method !== "HEAD") {
    // Buffer the body — Next.js NextRequest exposes a ReadableStream
    // but fetch in Node 20+ accepts only string/Buffer/Stream of
    // ArrayBuffer chunks. Buffer to text for safety; payloads are
    // small JSON.
    body = await req.text();
  }

  let upstream: Response;
  try {
    upstream = await fetch(upstreamURL, {
      method,
      headers: outHeaders,
      body,
      // SSE: never cache.
      cache: "no-store",
      // Hint Node fetch to keep the connection alive for streaming.
      // @ts-expect-error — `duplex` is Node-specific but required for
      // streaming the response body back; ignored by TS dom-fetch.
      duplex: "half",
    });
  } catch (err) {
    return NextResponse.json(
      {
        code: "bff_unreachable",
        message: `gateway-bff unreachable: ${(err as Error).message}`,
      },
      { status: 502 },
    );
  }

  if (opts.stream) {
    // SSE pass-through: return the upstream Response as-is so the
    // ReadableStream body + content-type: text/event-stream survive.
    // Sanitise hop-by-hop headers that confuse the browser when re-
    // emitted (e.g. transfer-encoding); Next will rewrite as needed.
    const headers = new Headers(upstream.headers);
    headers.delete("transfer-encoding");
    headers.delete("connection");
    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers,
    });
  }

  // Non-stream: buffer + re-emit. Preserves the upstream JSON code +
  // message envelope so ApiClient's error parser sees the same shape
  // it would have from a direct call.
  const text = await upstream.text();
  const respHeaders = new Headers();
  const ct = upstream.headers.get("content-type");
  if (ct) respHeaders.set("content-type", ct);
  return new Response(text, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: respHeaders,
  });
}

function trimSlash(s: string): string {
  return s.replace(/\/$/, "");
}
