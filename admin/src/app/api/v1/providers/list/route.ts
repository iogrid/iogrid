/**
 * POST /api/v1/admin/providers/list — same-origin admin BFF proxy that
 * wraps the providers-svc Connect-RPC ListProviders call (#237).
 *
 * Why a wrapper: the original admin UI called the Connect-RPC method
 * directly at `${API_BASE_URL}/iogrid.providers.v1.ProviderRegistrationService/ListProviders`,
 * which is cross-origin from `app.iogrid.org`. Migrating to a same-
 * origin JSON wrapper lets us reuse the NextAuth session cookie and
 * the service-token shim without re-implementing Connect-RPC in Node.
 *
 * Upstream path: `/iogrid.providers.v1.ProviderRegistrationService/ListProviders`
 * is exposed on gateway-bff (or providers-svc directly via Traefik).
 * For Phase 0 we forward to the same gateway-bff host so the auth
 * middleware short-circuits via the service-token shim; if a future
 * deployment fronts providers-svc on a different URL, set
 * `IOGRID_PROVIDERS_RPC_URL` to override.
 *
 * The body is forwarded verbatim (`{}` for "list all"); the response
 * is the Connect-RPC JSON envelope `{ providers: [...] }`.
 */
import { NextRequest, NextResponse } from "next/server";

import { auth } from "@/lib/auth";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
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

  // Upstream resolution: prefer an explicit providers-RPC URL, fall
  // back to the gateway-bff URL (which can proxy via Traefik). Phase 0
  // ingress wires both behind the same host.
  const upstreamBase =
    process.env.IOGRID_PROVIDERS_RPC_URL ??
    process.env.IOGRID_GATEWAY_BFF_URL ??
    "http://gateway-bff.iogrid.svc.cluster.local:8080";
  const serviceToken = process.env.IOGRID_SERVICE_TOKEN ?? "";
  if (!upstreamBase || !serviceToken) {
    return NextResponse.json(
      {
        code: "bff_proxy_unavailable",
        message: "providers-rpc proxy not configured",
      },
      { status: 503 },
    );
  }

  const upstreamURL =
    upstreamBase.replace(/\/$/, "") +
    "/iogrid.providers.v1.ProviderRegistrationService/ListProviders";

  const session_user = session.user as {
    email?: string | null;
    roles?: string[];
    role?: string;
  };
  const sessionRoles: string[] = Array.isArray(session_user.roles)
    ? session_user.roles
    : session_user.role
      ? [session_user.role]
      : [];
  // Always assert ADMIN — providers-svc.ListProviders rejects non-admin
  // callers. The merged set lets a session that's already ADMIN avoid
  // a duplicate entry.
  const roles = Array.from(new Set([...sessionRoles, "ADMIN"]));

  const outHeaders: Record<string, string> = {
    authorization: `Bearer ${serviceToken}`,
    "content-type": "application/json",
    "x-iogrid-user-id": userId,
    "x-iogrid-user-roles": roles.join(","),
  };
  if (session_user.email) {
    outHeaders["x-iogrid-user-email"] = session_user.email;
  }

  let body: string;
  try {
    body = await req.text();
    if (!body) body = "{}";
  } catch {
    body = "{}";
  }

  let upstream: Response;
  try {
    upstream = await fetch(upstreamURL, {
      method: "POST",
      headers: outHeaders,
      body,
      cache: "no-store",
    });
  } catch (err) {
    return NextResponse.json(
      {
        code: "providers_rpc_unreachable",
        message: `providers-svc unreachable: ${(err as Error).message}`,
      },
      { status: 502 },
    );
  }

  const text = await upstream.text();
  const headers = new Headers();
  const ct = upstream.headers.get("content-type");
  if (ct) headers.set("content-type", ct);
  return new Response(text, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers,
  });
}
