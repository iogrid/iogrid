/**
 * GET /api/v1/account/updates — same-origin BFF proxy (issue #289).
 *
 * Returns auto-update preferences + the latest channel state for the
 * operator-control surface (#59). Backed by gateway-bff `GetUpdates`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
