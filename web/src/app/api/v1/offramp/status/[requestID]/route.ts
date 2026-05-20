/**
 * GET /api/v1/offramp/status/[requestID] — same-origin BFF proxy (#289).
 *
 * Polls the status of an in-flight off-ramp request. Backed by
 * gateway-bff `GetOffRampStatus`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
