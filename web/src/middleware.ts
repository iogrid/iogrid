import NextAuth from "next-auth";
import { NextResponse } from "next/server";

import { authConfig } from "@/auth.config";

/**
 * Edge-runtime middleware — protects /provide, /customer.
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
 * The staff console moved to its own Next.js app at admin.iogrid.org
 * in #361 — that's why /admin is no longer in this matcher. Operators
 * hit the admin app by hostname; allowlist gating lives there.
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
