import NextAuth from "next-auth";
import { NextResponse } from "next/server";

import { authConfig } from "@/auth.config";

/**
 * Edge-runtime middleware — protects /provide, /customer, /admin and
 * enforces the host-split between `app.iogrid.org` and
 * `admin.iogrid.org` (#407).
 *
 * Imports the **edge-safe** `authConfig` (Google + JWT only). Importing
 * from `@/lib/auth` would transitively pull `nodemailer`, which uses
 * Node's `stream` module and crashes the edge runtime with
 *   `TypeError: Cannot redefine property: __import_unsupported`
 * on the first protected-route navigation. See #204.
 *
 * We use the `auth()` wrapper (returned by `NextAuth(authConfig)`) so we
 * get the parsed session attached to the request as `req.auth`, which
 * lets us run the redirect-with-`callbackUrl` flow without re-reading
 * the JWT ourselves.
 *
 * Host-aware admin routing (#407)
 * --------------------------------
 * After #383 reverted the standalone admin/ app, /admin/* lives in
 * web/. To still get cookie-scope isolation + a clean SSO surface, we
 * front the same `web` Service on a second Host header,
 * `admin.iogrid.org`, via `infra/k8s/traefik/ingressroute-admin.yaml`.
 * The middleware then enforces the canonical placement:
 *
 *   - `admin.iogrid.org/<anything>` for a non-admin user → 307 redirect
 *     to `app.iogrid.org/<same-path>` (with sign-in redirect first if
 *     unauthenticated). Non-admins have no business on admin.*; sending
 *     them to app.* lands them on their normal surface.
 *   - `app.iogrid.org/admin/<path>` for ANY user → 307 redirect to
 *     `admin.iogrid.org/admin/<path>`. Admin UI has one canonical host.
 *     Doing this for unauthenticated users too means the sign-in form
 *     they see is the one bound to admin.iogrid.org, so the resulting
 *     session cookie is host-scoped to admin.* and stays out of app.*.
 *
 * Both redirects use 307 (Temporary Redirect, method-preserving) rather
 * than 301/308 so a POST that lands on the wrong host doesn't get its
 * body silently dropped. The forwarded host is read off `req.headers`
 * via `req.nextUrl.host` (which Next.js populates from the trusted
 * x-forwarded-host header when behind a proxy).
 *
 * If `IOGRID_ADMIN_HOST` / `IOGRID_APP_HOST` env vars are unset (e.g.
 * `pnpm dev` on localhost), the host-split logic short-circuits so the
 * local dev experience continues to work via the path-based gate alone.
 */
const { auth } = NextAuth(authConfig);

const PROTECTED_PREFIXES = ["/provide", "/customer", "/admin"];

const ADMIN_HOST = (process.env.IOGRID_ADMIN_HOST ?? "admin.iogrid.org")
  .toLowerCase();
const APP_HOST = (process.env.IOGRID_APP_HOST ?? "app.iogrid.org")
  .toLowerCase();

// /admin is restricted to addresses in IOGRID_ADMIN_EMAILS (comma-sep).
// The middleware runs in the edge runtime — we can only read the bearer
// JWT claims (no DB), so the canonical role check happens at the BFF.
// This is a defense-in-depth pre-check that avoids rendering /admin
// to unauthenticated/non-admin users.
function adminEmails(): Set<string> {
  const raw = process.env.IOGRID_ADMIN_EMAILS ?? "";
  return new Set(
    raw
      .split(",")
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean),
  );
}

function isAdminEmail(email: string | null | undefined): boolean {
  if (!email) return false;
  return adminEmails().has(email.toLowerCase());
}

export default auth(function middleware(req) {
  const { pathname } = req.nextUrl;
  const host = req.nextUrl.host.toLowerCase();
  const onAdminHost = host === ADMIN_HOST;
  const onAppHost = host === APP_HOST;

  const email = req.auth?.user?.email ?? null;
  const isAdmin = isAdminEmail(email);

  // Host-split: app.iogrid.org/admin/<path> → admin.iogrid.org/admin/<path>.
  // Done BEFORE the auth check so unauthenticated users get redirected to
  // admin.* first and then prompted to sign in there, keeping the resulting
  // session cookie host-scoped to admin.iogrid.org.
  if (
    onAppHost &&
    (pathname === "/admin" || pathname.startsWith("/admin/"))
  ) {
    const url = req.nextUrl.clone();
    url.host = ADMIN_HOST;
    url.port = "";
    return NextResponse.redirect(url, 307);
  }

  // Host-split: admin.iogrid.org/<anything> for a non-admin → bounce to
  // app.iogrid.org/<same-path>. Authenticated-but-non-admin users get
  // sent to /customer with a from=admin-forbidden hint; unauthenticated
  // users get sent to admin.iogrid.org/account so they can sign in on
  // the admin host and then re-evaluate.
  if (onAdminHost) {
    if (!req.auth?.user) {
      const url = req.nextUrl.clone();
      url.pathname = "/account";
      url.searchParams.set("callbackUrl", pathname);
      return NextResponse.redirect(url);
    }
    if (!isAdmin) {
      const url = req.nextUrl.clone();
      url.host = APP_HOST;
      url.port = "";
      url.pathname = "/customer";
      url.searchParams.set("from", "admin-forbidden");
      return NextResponse.redirect(url, 307);
    }
    // Admin on admin host — fall through to standard checks.
  }

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

  // /admin: require IOGRID_ADMIN_EMAILS allowlist match. Unauthorized
  // sessions get sent to /customer (their default surface) rather than
  // back to /account — they're already signed in.
  if (pathname === "/admin" || pathname.startsWith("/admin/")) {
    if (!isAdmin) {
      const url = req.nextUrl.clone();
      url.pathname = "/customer";
      url.searchParams.set("from", "admin-forbidden");
      return NextResponse.redirect(url);
    }
  }

  return NextResponse.next();
});

export const config = {
  // Matcher includes "/" so the admin-host non-admin bounce fires on
  // top-level navigations to admin.iogrid.org/ (not just /admin/*). The
  // path-protection branches still only fire for /provide, /customer,
  // /admin per PROTECTED_PREFIXES — everything else falls through.
  matcher: [
    "/",
    "/admin/:path*",
    "/customer/:path*",
    "/provide/:path*",
  ],
};
