/**
 * DELETE /api/v1/account/sessions/[id] — same-origin BFF proxy
 * (issue #322).
 *
 * Soft-revokes a single session id owned by the caller. Ownership
 * and "cannot revoke your own current session" are enforced by
 * identity-svc (via AuthService.RevokeSession); this route only
 * forwards.
 *
 * Surface mapping:
 *   204 / 200 — revoked
 *   401      — unauthenticated
 *   404      — session not found or not owned by caller
 *   409      — caller tried to revoke their own current session
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function DELETE(req: NextRequest) {
  // Path is forwarded verbatim — gateway-bff has matching
  // DELETE /api/v1/account/sessions/{id} wired in routes.go.
  return proxyToBff(req);
}
