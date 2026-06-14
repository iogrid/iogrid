import { createHash } from "node:crypto";
import { and, eq } from "drizzle-orm";
import { cookies, headers } from "next/headers";
import { NextResponse } from "next/server";

import { auth } from "@/lib/auth";
import { db } from "@/db/client";
import { sessions } from "@/db/schema";

/**
 * Web (NextAuth) sessions feed — the "this device" row (#685 follow-up,
 * TC-09 watch-item; root cause finished in #808).
 *
 * /account/sessions previously listed ONLY identity-svc AuthService
 * sessions, which are empty for NextAuth-authenticated users (web
 * sessions live OUTSIDE identity-svc — the same architecture divergence
 * #685 fixed for identifiers). This route surfaces the web's own current
 * session so the panel can always show at least "this device".
 *
 * #808 — why the original `022876b3` implementation was a no-op in prod:
 *   That version listed ONLY rows from the Drizzle `session` table
 *   (`db.select().from(sessions)`). But `auth.config.ts` sets
 *   `session: { strategy: "jwt" }`, and under JWT strategy NextAuth NEVER
 *   writes rows to that table — the session lives entirely in the
 *   `__Secure-authjs.session-token` cookie as a signed JWT. So the query
 *   returned `[]`, no current row ever rendered, and the panel fell back
 *   to the stale identity-svc rows (all `is_current=false`, blank UA, past
 *   `expires_at`) → "Unknown device · Expired".
 *
 * Fix: the authoritative "this device" signal under JWT strategy is the
 * LIVE NextAuth session (`auth()`), which always exists for an authed
 * request. We synthesize the current-device row from it (real UA, real
 * `expires`, `is_current:true`). The Drizzle-table scan is retained for
 * forward-compat: if the app ever switches to `strategy:"database"`, the
 * persisted OTHER-device rows surface again automatically — but the
 * cookie-derived current row is never duplicated (we drop the table row
 * whose hash equals the current cookie's).
 *
 * Session tokens are NEVER returned: rows are keyed by a sha256 prefix
 * of the token, and revocation recomputes the hash server-side.
 *
 *   GET    /account/sessions/web-sessions          -> {sessions:[...]}
 *   DELETE /account/sessions/web-sessions?id=<hash> -> 204 (revokes one
 *          non-current web session belonging to the caller)
 */

function tokenHash(token: string): string {
  return createHash("sha256").update(token).digest("hex").slice(0, 16);
}

async function currentToken(): Promise<string> {
  const jar = await cookies();
  return (
    jar.get("__Secure-authjs.session-token")?.value ??
    jar.get("authjs.session-token")?.value ??
    ""
  );
}

// Stable id for the cookie-derived current-device row. When the JWT
// session-token cookie is present we key off its hash (consistent with
// the DELETE-revocation hashing). If it's somehow absent (e.g. a
// header-based session in a future config) fall back to a fixed sentinel
// so the row still renders + is recognisable as the current device.
function currentRowId(cookieToken: string): string {
  return cookieToken ? tokenHash(cookieToken) : "current-device";
}

export async function GET(_req: Request) {
  const session = await auth();
  const userId = session?.user?.id;
  if (!userId) {
    return NextResponse.json({ sessions: [] }, { status: 401 });
  }

  const cookieToken = await currentToken();
  const currentId = currentRowId(cookieToken);
  const ua = (await headers()).get("user-agent") ?? "";

  // The live NextAuth session is the source of truth for "this device".
  // `session.expires` is the JWT expiry (ISO-8601 string); fall back to
  // an empty string so the panel simply omits the "expires" pill rather
  // than rendering a bogus one.
  const currentRow = {
    id: currentId,
    is_current: true,
    user_agent: ua,
    expires_at: session.expires ?? "",
    kind: "web",
  };

  // Forward-compat: surface any PERSISTED web sessions too (only
  // non-empty under `strategy:"database"`). Drop the row that maps to the
  // current cookie so the current device is never listed twice. Best-
  // effort: a DB hiccup must not blank the (already-known) current row.
  let otherRows: Array<{
    id: string;
    is_current: boolean;
    user_agent: string;
    expires_at: string;
    kind: string;
  }> = [];
  if (db) {
    try {
      const rows = await db
        .select({
          sessionToken: sessions.sessionToken,
          expires: sessions.expires,
        })
        .from(sessions)
        .where(eq(sessions.userId, userId));
      otherRows = rows
        .map((r) => ({ id: tokenHash(r.sessionToken), expires: r.expires }))
        .filter((r) => r.id !== currentId)
        .map((r) => ({
          id: r.id,
          is_current: false,
          // The NextAuth session table stores no UA/IP — only the current
          // request's UA can be truthfully attributed (and that row is
          // the synthesized one above).
          user_agent: "",
          expires_at: r.expires.toISOString(),
          kind: "web",
        }));
    } catch {
      otherRows = [];
    }
  }

  return NextResponse.json({ sessions: [currentRow, ...otherRows] });
}

export async function DELETE(req: Request) {
  const session = await auth();
  const userId = session?.user?.id;
  if (!userId || !db) {
    return NextResponse.json(
      { error: "unauthenticated" },
      { status: userId ? 503 : 401 },
    );
  }
  const id = new URL(req.url).searchParams.get("id") ?? "";
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
  }
  if (id === currentRowId(await currentToken())) {
    return NextResponse.json(
      { error: "sign out instead to end the current session" },
      { status: 409 },
    );
  }
  // Recompute hashes over the caller's OWN rows only — a guessed hash
  // can never touch another user's session.
  const rows = await db
    .select({ sessionToken: sessions.sessionToken })
    .from(sessions)
    .where(eq(sessions.userId, userId));
  const match = rows.find((r) => tokenHash(r.sessionToken) === id);
  if (!match) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  await db
    .delete(sessions)
    .where(
      and(
        eq(sessions.userId, userId),
        eq(sessions.sessionToken, match.sessionToken),
      ),
    );
  return new NextResponse(null, { status: 204 });
}
