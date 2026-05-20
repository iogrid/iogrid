/**
 * /api/v1/account/wallets — same-origin BFF proxy (issue #326).
 *
 * GET  — list Solana wallets bound to the calling user.
 * POST — finish the SIWS binding handshake (caller signed the nonce
 *        returned by /challenge below; this submits the signature so
 *        identity-svc can verify + persist).
 *
 * Replaces the Phase 0 stub at /api/v1/identity/wallets which always
 * returned an empty list and 501'd on the mutating verbs. The new
 * surface is the canonical wallet-management endpoint; the UI's
 * `web/src/lib/solana/siws.ts` helper is repointed accordingly.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
