import { NextRequest, NextResponse } from "next/server";

/**
 * /onboard/redirect?token=ABC123 — bridge from the manual-entry form on
 * /onboard to the per-token page at /onboard/[token]. Lives as a
 * Route Handler so the form can use plain GET with no JS.
 *
 * Validates the token shape before redirecting; malformed codes bounce
 * back to /onboard with an error query param.
 */
const PAIRING_CODE_RE = /^[0-9A-HJ-NP-TV-Z]{6}$/;

export async function GET(req: NextRequest) {
  const token = (req.nextUrl.searchParams.get("token") ?? "")
    .trim()
    .toUpperCase();
  if (!PAIRING_CODE_RE.test(token)) {
    const url = req.nextUrl.clone();
    url.pathname = "/onboard";
    url.searchParams.set("error", "invalid_code");
    url.searchParams.delete("token");
    return NextResponse.redirect(url);
  }
  const url = req.nextUrl.clone();
  url.pathname = `/onboard/${token}`;
  url.searchParams.delete("token");
  return NextResponse.redirect(url);
}
