/**
 * PATCH/DELETE /api/v1/workspaces/[id]/members/[userID] — BFF proxy (#289).
 *
 * Updates a member's role or removes them from the workspace. Backed by
 * gateway-bff `UpdateMemberRole` / `RemoveMember`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function PATCH(req: NextRequest) {
  return proxyToBff(req);
}

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
