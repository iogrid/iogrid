/**
 * POST /api/v1/offramp/start — same-origin BFF proxy (issue #289).
 *
 * Mints a partner redirect URL to begin an off-ramp withdrawal. Backed
 * by gateway-bff `StartOffRamp`. Surfaces on /provide/earnings/withdraw.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
