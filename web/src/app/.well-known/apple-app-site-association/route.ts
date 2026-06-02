import { NextResponse } from "next/server";

/**
 * GET /.well-known/apple-app-site-association — Apple App Site Association
 * (AASA) for iogrid Universal Links.
 *
 * Required for the Ping integration "Direction B" handshake (coordination
 * item C-6): Ping launches `https://iogrid.org/buy-vpn?...&return_url=
 * https://ping.cash/vpn-confirmed`, and iOS only routes that URL straight
 * into the iogrid app (no "Open in 'iogrid'?" dialog) when this file is
 * served from `iogrid.org/.well-known/apple-app-site-association`.
 *
 * Apple's `swcd` daemon fetches this over HTTPS and is STRICT:
 *   - Content-Type MUST be `application/json` (NOT text/plain).
 *   - The URL MUST have NO file extension.
 *   - The response MUST be unauthenticated, no redirects.
 * A static file under `public/.well-known/` can be served with the wrong
 * MIME type by some edge/CDN layers, so this route handler is the robust
 * path that pins the Content-Type explicitly. The body is kept in sync
 * with `web/public/.well-known/apple-app-site-association`.
 *
 * Contract: docs/MULTI_TENANT_MATRIX.md + ping-cash:docs/coordination/
 * iogrid-ping-integration.md. Tracked in issue #629.
 */

// Apple Developer Team ID. PLACEHOLDER until the iogrid Foundation holds an
// Apple Developer account — see docs/MULTI_TENANT_MATRIX.md "What remains
// blocked". Overridable via env so a real value can be injected at deploy
// WITHOUT a code change. DO NOT hard-code a real Team ID here.
const APPLE_TEAM_ID = process.env.NEXT_PUBLIC_APPLE_TEAM_ID ?? "PLACEHOLDER_TEAMID";

// iOS bundle identifier for the iogrid app (stable; see CLAUDE.md mobile).
const APP_BUNDLE_ID = "io.iogrid.app";

const APP_ID = `${APPLE_TEAM_ID}.${APP_BUNDLE_ID}`;

/**
 * Build the AASA document. Paths cover the Direction-B entry (`/buy-vpn`)
 * and the VPN activation surface (`/vpn`), matching the scheme table in
 * docs/MULTI_TENANT_MATRIX.md.
 */
function buildAasa() {
  return {
    applinks: {
      apps: [] as string[],
      details: [
        {
          appID: APP_ID,
          appIDs: [APP_ID],
          paths: ["/buy-vpn", "/buy-vpn/*", "/vpn", "/vpn/*"],
          components: [
            { "/": "/buy-vpn" },
            { "/": "/buy-vpn/*" },
            { "/": "/vpn" },
            { "/": "/vpn/*" },
          ],
        },
      ],
    },
    webcredentials: {
      apps: [APP_ID],
    },
  };
}

// AASA content is deterministic per deploy; allow static optimisation.
export const dynamic = "force-static";

export function GET() {
  return NextResponse.json(buildAasa(), {
    headers: {
      // Apple's swcd requires application/json; pin it explicitly.
      "Content-Type": "application/json",
      // Cache at the edge; Apple re-fetches periodically.
      "Cache-Control": "public, max-age=3600",
    },
  });
}

// Exported for unit tests (see src/test/aasa.test.ts).
export { buildAasa, APP_ID, APP_BUNDLE_ID, APPLE_TEAM_ID };
