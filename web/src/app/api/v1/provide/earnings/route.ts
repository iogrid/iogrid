/**
 * GET /api/v1/provide/earnings — same-origin BFF proxy (#237).
 *
 * Forwards `?start=…&end=…&provider_id=…` query params verbatim.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
