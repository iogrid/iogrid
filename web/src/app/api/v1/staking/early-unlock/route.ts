/**
 * POST /api/v1/staking/early-unlock — same-origin BFF proxy (issue #289).
 *
 * Triggers an early unlock (50% burn) on a $GRID stake position.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
