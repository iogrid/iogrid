/**
 * GET /api/v1/customer/usage — same-origin BFF proxy (issue #244).
 *
 * Reads the NextAuth session, forwards to gateway-bff with the
 * IOGRID_SERVICE_TOKEN + X-Iogrid-User-Id shim. See
 * `web/src/lib/bff-proxy.ts` for the full auth model.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
