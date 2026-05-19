/**
 * GET /api/v1/customer/workloads/[id]/events — SSE pass-through (#244).
 *
 * Streams workload state changes from gateway-bff back to the browser
 * EventSource. Uses bff-proxy's `stream: true` to preserve the
 * `text/event-stream` content-type + ReadableStream body.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req, { stream: true });
}
