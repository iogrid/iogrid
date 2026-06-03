/**
 * GET /api/v1/customer/vpn/sessions — same-origin BFF proxy (issue #644).
 *
 * Reads the NextAuth session, forwards to gateway-bff with the
 * IOGRID_SERVICE_TOKEN + X-Iogrid-User-Id shim. The gateway-bff handler
 * (customer.go ListCustomerVPNSessions) derives customer_id from the
 * authenticated session — the workspace_id query param is forwarded
 * untouched. Mirrors customer/usage/route.ts. See
 * `web/src/lib/bff-proxy.ts` for the full auth model.
 *
 * Without this route the customer/vpn panel's fetch hit Next.js's default
 * 404 and silently rendered an always-empty sessions list (#644, same
 * class as #641 / #289).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
