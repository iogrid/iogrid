/**
 * GET /api/v1/admin/abuse-queue — same-origin admin BFF proxy (#237).
 *
 * The Next.js BFF asserts ADMIN role via X-Iogrid-User-Roles so
 * gateway-bff's RequireRole("ADMIN") middleware accepts the
 * materialised Claims. Cross-checking the session role is the
 * gateway-bff handler's job (mustAdmin re-checks defence-in-depth).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req, { extraRoles: ["ADMIN"] });
}
