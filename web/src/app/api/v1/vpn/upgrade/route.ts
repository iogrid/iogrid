/**
 * POST /api/v1/vpn/upgrade — same-origin BFF proxy (issue #289).
 *
 * Starts a Stripe Checkout session for VPN plan upgrade. Backed by
 * gateway-bff `UpgradeVPN`. Surfaces on /vpn/upgrade + /customer/billing.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
