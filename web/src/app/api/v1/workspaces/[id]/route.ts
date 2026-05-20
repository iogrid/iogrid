/**
 * GET/PATCH/DELETE /api/v1/workspaces/[id] — same-origin BFF proxy (#289).
 *
 * Per-workspace operations: get details, rename, delete. Backed by
 * gateway-bff `GetWorkspace` / `UpdateWorkspace` / `DeleteWorkspace`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}

export async function PATCH(req: NextRequest) {
  return proxyToBff(req);
}

export async function DELETE(req: NextRequest) {
  return proxyToBff(req);
}
