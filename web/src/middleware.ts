import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";
import { auth } from "@/lib/auth";

const PROTECTED_PREFIXES = ["/provide", "/customer", "/admin"];

export async function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  const isProtected = PROTECTED_PREFIXES.some(
    (p) => pathname === p || pathname.startsWith(`${p}/`),
  );

  if (!isProtected) {
    return NextResponse.next();
  }

  const session = await auth();
  if (!session?.user) {
    const url = req.nextUrl.clone();
    url.pathname = "/account";
    url.searchParams.set("callbackUrl", pathname);
    return NextResponse.redirect(url);
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/provide/:path*", "/customer/:path*", "/admin/:path*"],
};
