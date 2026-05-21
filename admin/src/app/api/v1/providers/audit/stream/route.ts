/**
 * GET /api/v1/providers/audit/stream — same-origin SSE pass-through for
 * the admin per-provider transparency audit (EPIC #422 Phase 1).
 *
 * Mirror of web/'s /api/v1/provide/audit/stream — but the admin app
 * routes audit lookups through its own admin-scoped path so EventSource
 * stays on admin.iogrid.org and inherits the host-scoped admin cookie.
 *
 * Forwarded upstream path overrides the same-host default so the
 * gateway-bff streaming RPC keeps its existing handler.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";
// Long-lived SSE — keep-alive frames keep the connection open.
export const maxDuration = 300;

export async function GET(req: NextRequest) {
  const search = req.nextUrl.search; // ?provider_id=...
  return proxyToBff(req, {
    stream: true,
    extraRoles: ["ADMIN"],
    upstreamPath: `/api/v1/provide/audit/stream${search}`,
  });
}
