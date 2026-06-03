/**
 * GET /api/v1/staking/ — same-origin BFF proxy (issue #644).
 *
 * Backs the /provider/staking page's base-state read. gateway-bff
 * (routes.go: emptyStakingState) returns the Phase-0 "opted-out, zero
 * stake" snapshot. The sibling sub-routes (positions, claim, stake,
 * early-unlock) already have proxies; only the root GET was missing.
 *
 * Without this route the GET hit Next.js's default 404 (#644, same class
 * as #641 / #289). Mirrors staking/positions/route.ts.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
