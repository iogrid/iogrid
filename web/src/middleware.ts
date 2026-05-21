import NextAuth from "next-auth";
import { NextResponse } from "next/server";

import { authConfig } from "@/auth.config";

/**
 * Edge-runtime middleware — protects /provide and /customer (the
 * user-facing protected surfaces).
 *
 * Imports the **edge-safe** `authConfig` (Google + JWT only). Importing
 * from `@/lib/auth` would transitively pull `nodemailer`, which uses
 * Node's `stream` module and crashes the edge runtime with
 *   `TypeError: Cannot redefine property: __import_unsupported`
 * on the first protected-route navigation. See #204.
 *
 * Admin surfaces now live in the independent admin/ app on
 * admin.iogrid.org (EPIC #422 Phase 1). web/ no longer carries /admin
 * routes or host-aware admin redirects — the host split is enforced
 * at the IngressRoute layer (admin.iogrid.org → admin Service,
 * iogrid.org → web Service) with separate cookie scopes per host. If
 * a stale link sends a user to /admin on web/, Next.js renders the
 * standard 404 — admin is not findable here.
 *
 * EPIC #422 Phase 3 dropped the app.iogrid.org subdomain entirely;
 * the product app moved to the apex (iogrid.org). app.iogrid.org now
 * 308-redirects to iogrid.org (path+query preserved) — see
 * `infra/k8s/traefik/ingressroute-app-redirect.yaml`.
 */
const { auth } = NextAuth(authConfig);

const PROTECTED_PREFIXES = ["/provide", "/customer"];

export default auth(function middleware(req) {
  const { pathname } = req.nextUrl;

  const isProtected = PROTECTED_PREFIXES.some(
    (p) => pathname === p || pathname.startsWith(`${p}/`),
  );

  if (!isProtected) {
    return NextResponse.next();
  }

  if (!req.auth?.user) {
    const url = req.nextUrl.clone();
    url.pathname = "/account";
    url.searchParams.set("callbackUrl", pathname);
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
});

export const config = {
  matcher: ["/provide/:path*", "/customer/:path*"],
};
