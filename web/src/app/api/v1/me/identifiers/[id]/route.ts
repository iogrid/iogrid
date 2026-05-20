/**
 * DELETE /api/v1/me/identifiers/[id] — same-origin BFF proxy (issue #289).
 *
 * Unbinds one identifier (email/google/wallet) from the calling user's
 * identity-svc account. Backed by gateway-bff `RemoveMyIdentifier` (#196).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
