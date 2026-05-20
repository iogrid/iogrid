/**
 * GET/DELETE /api/v1/me — same-origin BFF proxy (issue #289).
 *
 * GET → gateway-bff `GetMe` (returns identity-svc claims + bound identifiers).
 * DELETE → gateway-bff `DeleteMyAccount` (soft-delete cascade; #197).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
