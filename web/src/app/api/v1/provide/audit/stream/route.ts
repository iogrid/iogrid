/**
 * GET /api/v1/provide/audit/stream — same-origin SSE pass-through (#237).
 *
 * EventSource pins to same-origin to inherit the NextAuth cookie. The
 * proxy forwards to gateway-bff and streams `text/event-stream` frames
 * back without buffering.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";
// Long-lived SSE — Vercel-style fluid functions need an explicit
// max-duration override; on a self-hosted Node server this is a no-op
// but documents intent. The keep-alive frames keep the conn open.
export const maxDuration = 300;

export async function GET(req: NextRequest) {
  return proxyToBff(req, { stream: true });
}
