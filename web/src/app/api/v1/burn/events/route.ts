/**
 * GET /api/v1/burn/events — same-origin BFF proxy (issue #289).
 *
 * Solana $GRID burn event feed (see `web/src/lib/solana/burn.ts`).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
