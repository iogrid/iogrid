/**
 * POST /api/v1/identity/wallets/complete-binding — same-origin BFF proxy (#289).
 *
 * SIWS step 2: client posts the signed message; backend verifies and
 * binds the wallet to the user's identity-svc account.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
