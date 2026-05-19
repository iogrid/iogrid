/**
 * GET/POST /api/v1/customer/api-keys — same-origin BFF proxy (issue #244).
 *
 * GET → gateway-bff `ListAPIKeys`
 * POST → gateway-bff `CreateAPIKey`
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
