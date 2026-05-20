/**
 * GET /api/v1/staking/positions — same-origin BFF proxy (issue #289).
 *
 * Lists active $GRID stake positions for the caller (see
 * `web/src/lib/solana/staking.ts`).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
