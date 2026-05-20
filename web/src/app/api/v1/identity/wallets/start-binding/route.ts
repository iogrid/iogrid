/**
 * POST /api/v1/identity/wallets/start-binding — same-origin BFF proxy (#289).
 *
 * SIWS step 1: server issues a nonce + canonical message for the wallet
 * to sign. See `web/src/lib/solana/siws.ts`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
