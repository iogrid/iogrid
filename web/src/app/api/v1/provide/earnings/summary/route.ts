/**
 * GET /api/v1/provide/earnings/summary — same-origin BFF proxy (#324).
 *
 * Forwards to gateway-bff which fans onto billing-svc.GetEarningsSummary.
 * The headline-card surface for /provider/earnings — lifetime, last-30d,
 * last-7d, pending-payout, lifetime-workloads — distinct from the
 * windowed providers-svc shape served by `/provider/earnings`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
