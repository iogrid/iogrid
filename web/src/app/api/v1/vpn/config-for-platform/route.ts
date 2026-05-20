/**
 * GET /api/v1/vpn/config-for-platform — same-origin BFF proxy (issue #289).
 *
 * Streams the per-platform VPN artefact (.conf | .mobileconfig | QR
 * payload) straight to the browser. Backed by gateway-bff
 * `GetVPNConfigForPlatform`. Surfaces on /vpn (downloads panel).
 *
 * Uses stream pass-through so the upstream content-type + binary body
 * survive intact.
 */
import { NextRequest } from "next/server";

import { proxyToBff } from "@/lib/bff-proxy";

export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  return proxyToBff(req, { stream: true });
}
