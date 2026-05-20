/**
 * POST /api/v1/account/sign-out — same-origin BFF proxy (issue #289).
 *
 * Revokes a session at identity-svc. Backed by gateway-bff `SignOut`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
