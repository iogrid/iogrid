/**
 * GET /api/v1/customer/billing/balance — same-origin BFF proxy (#632).
 *
 * Returns the calling customer's prepaid $GRID balance + grace-overage
 * arrears for the /customer/billing surface. Backed by gateway-bff
 * GetCustomerBalance, which resolves the bound wallet and reads
 * billing-svc /v1/grid/balance.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
