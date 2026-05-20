/**
 * GET /api/v1/offramp/providers — same-origin BFF proxy (issue #289).
 *
 * Lists registered off-ramp partner providers. Public route on the
 * gateway-bff side; surfaces on /provide/earnings/withdraw (#167/#169).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
