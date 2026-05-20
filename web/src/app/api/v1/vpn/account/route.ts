/**
 * GET /api/v1/vpn/account — same-origin BFF proxy (issue #289).
 *
 * Returns the calling user's VPN account state (plan, expiry, etc.).
 * Backed by gateway-bff `GetVPNAccount`. Surfaces on /customer/billing
 * + /vpn.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
