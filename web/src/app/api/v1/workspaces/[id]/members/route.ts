/**
 * GET/POST /api/v1/workspaces/[id]/members — same-origin BFF proxy (#289).
 *
 * Lists / adds members for a workspace. Backed by gateway-bff
 * `ListMembers` / `AddMember`.
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
