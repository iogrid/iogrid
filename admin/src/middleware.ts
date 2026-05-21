import NextAuth from "next-auth";
import { NextResponse } from "next/server";

import { authConfig } from "@/auth.config";
import { isAdminEmail } from "@/lib/admin-allowlist";

/**
 * Edge-runtime middleware — gates the ENTIRE admin app on the
 * IOGRID_ADMIN_EMAILS allowlist.
 *
 * Imports the edge-safe `authConfig` (no nodemailer / pg). The canonical
 * role check still happens at the BFF on every action; this middleware
 * is defense-in-depth so non-admin sessions never even render the admin
 * shell.
 *
 * Unauthenticated  →  /signin?callbackUrl=<path>
 * Authenticated but not allowlisted  →  403 JSON response (no app.iogrid.org
 * redirect: this is a different host and we don't want to silently send the
 * operator to a different domain).
 */
const { auth } = NextAuth(authConfig);

const PUBLIC_PATHS = new Set<string>([
  "/signin",
  "/api/auth",
  "/healthz",
  "/readyz",
]);

function isPublic(pathname: string): boolean {
  if (PUBLIC_PATHS.has(pathname)) return true;
  // NextAuth handler lives at /api/auth/* — let every sub-path through.
  if (pathname.startsWith("/api/auth/")) return true;
  // Next.js internals + static assets.
  if (pathname.startsWith("/_next/")) return true;
  if (pathname === "/favicon.ico") return true;
  return false;
}

export default auth(function middleware(req) {
  const { pathname } = req.nextUrl;

  if (isPublic(pathname)) {
    return NextResponse.next();
  }

  if (!req.auth?.user) {
    const url = req.nextUrl.clone();
    url.pathname = "/signin";
    url.searchParams.set("callbackUrl", pathname);
    return NextResponse.redirect(url);
  }

  if (!isAdminEmail(req.auth.user.email, process.env.IOGRID_ADMIN_EMAILS)) {
    return new NextResponse(
      JSON.stringify({
        error: "forbidden",
        message:
          "This account is not on the iogrid admin allowlist. Contact your operator if you believe this is in error.",
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
  // Match everything except the static assets + NextAuth handler. The
  // middleware function above re-checks the path against PUBLIC_PATHS
  // so /signin and /api/auth/* still render even for unauthenticated
  // visitors.
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
