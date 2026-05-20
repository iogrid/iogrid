/**
 * GET /api/v1/identity/wallets — same-origin BFF proxy (issue #289).
 *
 * Lists wallets bound to the calling user's identity-svc account (SIWS;
 * see `web/src/lib/solana/siws.ts`). Forwarded to gateway-bff verbatim.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
