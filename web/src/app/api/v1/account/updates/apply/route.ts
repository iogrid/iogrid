/**
 * POST /api/v1/account/updates/apply — same-origin BFF proxy (issue #289).
 *
 * Applies a pending update. Backed by gateway-bff `ApplyPendingUpdate`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
