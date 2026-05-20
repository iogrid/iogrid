/**
 * GET /api/v1/account/sessions — same-origin BFF proxy (issue #289).
 *
 * Lists the calling user's active sessions. Backed by gateway-bff
 * `ListSessions`. Surfaces on /account/sessions.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
