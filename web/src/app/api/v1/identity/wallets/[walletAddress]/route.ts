/**
 * DELETE /api/v1/identity/wallets/[walletAddress] — same-origin BFF proxy (#289).
 *
 * Unbinds a wallet from the calling user's identity-svc account.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
