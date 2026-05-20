/**
 * GET/POST /api/v1/workspaces — same-origin BFF proxy (issue #289).
 *
 * Workspace bounded-context (#146). GET lists workspaces the caller is a
 * member of; POST creates a new workspace. Backed by gateway-bff
 * `ListWorkspaces` / `CreateWorkspace`. Surfaces on the workspace
 * selector in the global layout chrome.
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
