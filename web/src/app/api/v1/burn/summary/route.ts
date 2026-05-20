/**
 * GET /api/v1/burn/summary — same-origin BFF proxy (issue #289).
 *
 * Solana $GRID burn aggregate (see `web/src/lib/solana/burn.ts`). If
 * gateway-bff hasn't shipped the backend yet, the upstream code (404 /
 * 503) is surfaced to the caller — better than a Next.js 404 HTML page.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
