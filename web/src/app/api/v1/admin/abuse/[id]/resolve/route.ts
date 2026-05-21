/**
 * POST /api/v1/admin/abuse/{id}/resolve — same-origin admin BFF proxy
 * (issue #237). Body is `{ decision: "allow"|"block", note }`.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req, { extraRoles: ["ADMIN"] });
}
