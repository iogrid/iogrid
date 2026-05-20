/**
 * GET /api/v1/customer/billing — same-origin BFF proxy (issue #296).
 *
 * Surfaces the customer's subscription tier + bandwidth quota +
 * Stripe portal URL for the /customer/billing page. Phase 0 returns
 * a FREE/trial empty-state from gateway-bff; Phase 1 wires this to
 * billing-svc's subscription read model.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
