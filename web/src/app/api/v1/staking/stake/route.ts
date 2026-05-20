/**
 * POST /api/v1/staking/stake — same-origin BFF proxy (issue #289).
 *
 * Opens a new $GRID stake position.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
