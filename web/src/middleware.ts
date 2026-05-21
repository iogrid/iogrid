import NextAuth from "next-auth";
import { NextResponse } from "next/server";

import { authConfig } from "@/auth.config";

/**
 * Edge-runtime middleware — protects /provide, /customer, /admin.
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
 */
const { auth } = NextAuth(authConfig);

const PROTECTED_PREFIXES = ["/provide", "/customer", "/admin"];

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

  // /admin: require IOGRID_ADMIN_EMAILS allowlist match. Unauthorized
  // sessions get sent to /customer (their default surface) rather than
  // back to /account — they're already signed in.
  if (pathname === "/admin" || pathname.startsWith("/admin/")) {
    const email = (req.auth.user.email ?? "").toLowerCase();
    if (!email || !adminEmails().has(email)) {
      const url = req.nextUrl.clone();
      url.pathname = "/customer";
      url.searchParams.set("from", "admin-forbidden");
      return NextResponse.redirect(url);
    }
  }

  return NextResponse.next();
});

export const config = {
  matcher: ["/provide/:path*", "/customer/:path*", "/admin/:path*"],
};
