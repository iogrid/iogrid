/**
 * POST /api/v1/account/wallets/challenge — same-origin BFF proxy (#326).
 *
 * SIWS step 1: server issues a fresh nonce + canonical message bytes
 * for the wallet to ed25519-sign. The response feeds the wallet
 * adapter's `signMessage` call; the resulting signature is then POSTed
 * to /api/v1/account/wallets to complete the binding.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
