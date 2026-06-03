/**
 * POST + GET /api/v1/customer/workloads — same-origin BFF proxy (#244).
 * GET lists the workspace's workloads (#677 — before this only POST was
 * exported, so the dispatch list 405'd and the UI could only show a
 * browser-local "recent" list that vanished on refresh).
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function POST(req: NextRequest) {
  return proxyToBff(req);
}

export async function GET(req: NextRequest) {
  return proxyToBff(req);
}
