import NextAuth from "next-auth";
import { NextResponse } from "next/server";

import { authConfig } from "@/auth.config";

/**
 * Edge-runtime middleware for the admin/ app.
 *
 * Every route in admin/ is admin-only — the entire app IS the staff
 * console (EPIC #422 Phase 1). There are no user-facing surfaces to
 * gate against; the only checks are:
 *
 *   1. Signed in? Otherwise → /account (sign-in surface within this
 *      app, so the resulting cookie is admin-host-scoped).
 *   2. Email in IOGRID_ADMIN_EMAILS allowlist? Otherwise → 403
 *      (this is admin-only — there is no fallback "customer" surface
 *      to bounce to; that lives on iogrid.org / app.iogrid.org).
 *
 * Sign-in and NextAuth callback routes are excluded from the matcher
 * so the auth flow itself isn't gated by an auth check.
 *
 * Imports the edge-safe `authConfig` (Google + JWT only) — importing
 * from `@/lib/auth` would transitively pull `nodemailer` which uses
 * Node's `stream` module and crashes the edge runtime.
 */
const { auth } = NextAuth(authConfig);

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

  // Sign-in & NextAuth callback routes are NOT auth-gated — they ARE
  // the auth flow.
  if (
    pathname === "/account" ||
    pathname.startsWith("/account/") ||
    pathname.startsWith("/api/auth/")
  ) {
    return NextResponse.next();
  }

  if (!req.auth?.user) {
    const url = req.nextUrl.clone();
    url.pathname = "/account";
    url.searchParams.set("callbackUrl", pathname);
    return NextResponse.redirect(url);
  }

  if (!isAdminEmail(req.auth.user.email)) {
    // Unlike web/, there is NO non-admin surface in this app to bounce
    // to — admin.iogrid.org has no /customer / /vpn / /provide pages.
    // Return a hard 403 with a sign-out hint so the operator can
    // re-authenticate as the right account.
    return new NextResponse(
      JSON.stringify({
        code: "forbidden",
        message:
          "This email is not on the iogrid admin allowlist. Sign out and sign in with an admin account, or use app.iogrid.org for non-admin surfaces.",
      }),
      {
        status: 403,
        headers: { "content-type": "application/json" },
      },
    );
  }

  return NextResponse.next();
});

export const config = {
  // Match every request EXCEPT static assets + the readiness probes.
  matcher: [
    "/((?!_next/static|_next/image|favicon.ico|healthz|readyz).*)",
  ],
};
