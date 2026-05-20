/**
 * DELETE /api/v1/account/wallets/[address] — same-origin BFF proxy (#326).
 *
 * Unbinds a Solana wallet from the calling user. The {address} path
 * parameter is the base58 pubkey; identity-svc asserts ownership in
 * the WHERE clause (no separate SELECT) so a missing row is
 * indistinguishable from "not yours" — anti-enumeration.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
