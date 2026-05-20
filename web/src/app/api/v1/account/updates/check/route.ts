/**
 * POST /api/v1/account/updates/check — same-origin BFF proxy (issue #289).
 *
 * Triggers a fresh check against the release channel. Backed by
 * gateway-bff `TriggerUpdateCheck`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
