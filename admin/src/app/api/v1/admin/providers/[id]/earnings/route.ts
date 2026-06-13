/**
 * GET /api/v1/admin/providers/{id}/earnings — same-origin admin BFF
 * proxy (#758). Returns the billing-svc earnings headline for ANY
 * provider by UUID: lifetime / last_30d / last_7d / pending + the
 * settled on-chain $GRID half + the settled-build count.
 *
 * This is the OPERATOR surface: the founder's own account owns a
 * different daemon than the one that ran the real iOS builds, so the
 * owner-scoped /provide/earnings shows him $0 — this admin path lets
 * him SEE another provider's real settled $GRID (e.g. Hatice's
 * 808ce330 = 11.05 $GRID across 14 settled builds).
 *
 * The route path mirrors the gateway-bff path exactly so proxyToBff
 * forwards `reqUrl.pathname` verbatim and hits
 * gateway-bff's `/api/v1/admin/providers/{id}/earnings` (which is
 * gated by RequireRole("ADMIN") + a mustAdmin re-check). extraRoles
 * asserts ADMIN via X-Iogrid-User-Roles so that gate accepts the
 * materialised Claims.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req, { extraRoles: ["ADMIN"] });
}
