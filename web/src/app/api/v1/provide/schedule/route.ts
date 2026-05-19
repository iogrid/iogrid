/**
 * GET + POST /api/v1/provide/schedule — same-origin BFF proxy (#237).
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
