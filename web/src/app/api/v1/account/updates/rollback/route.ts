/**
 * POST /api/v1/account/updates/rollback — same-origin BFF proxy (issue #289).
 *
 * Rolls back to the previous version. Backed by gateway-bff `RollbackUpdate`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
