/**
 * GET + PUT /api/v1/provide/payout-method — same-origin BFF proxy (#324).
 *
 * Forwards to gateway-bff → billing-svc.{Get,Set}PayoutMethod. The
 * payout-method election is user-scoped (NOT provider-scoped) — one
 * consolidated preference per user across all their daemons.
 *
 * PUT body: { kind, destination_address?, charity_id? }
 *   kind ∈ "UNSPECIFIED" | "CASH_USDC" | "FREE_VPN" | "CHARITY"
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}

export async function PUT(req: NextRequest) {
  return proxyToBff(req);
}
