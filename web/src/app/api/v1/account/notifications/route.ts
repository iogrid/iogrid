/**
 * /api/v1/account/notifications — same-origin BFF proxy (#631 follow-up).
 *
 * GET  — read the calling user's notification-channel preferences.
 * POST — persist updated preferences.
 *
 * #631 shipped the page (account/notifications/panel.tsx), the gateway-bff
 * handler (notifications.go), AND the BFF route registration, but MISSED this
 * web-side proxy route — so the panel's same-origin fetch to
 * /api/v1/account/notifications hit the Next.js app (which serves iogrid.org/*)
 * with no matching route and 404'd. This forwards GET/POST to gateway-bff's
 * /api/v1/account/notifications, which proxies to identity-svc's
 * /v1/me/notification-prefs.
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
