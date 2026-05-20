/**
 * POST /api/v1/account/updates/preferences — same-origin BFF proxy (issue #289).
 *
 * Persists the operator's auto-update preferences. Backed by gateway-bff
 * `SaveUpdatePreferences`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
