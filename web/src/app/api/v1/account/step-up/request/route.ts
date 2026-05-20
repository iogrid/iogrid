/**
 * POST /api/v1/account/step-up/request — same-origin BFF proxy (issue #289).
 *
 * Requests a step-up auth challenge before a sensitive operation (e.g.
 * account deletion from /account/danger-zone). Forwarded to gateway-bff;
 * if the upstream returns 404 the panel surfaces the same upstream code
 * to the operator instead of a Next.js 404 HTML page.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}
