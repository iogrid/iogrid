import { createHash } from "node:crypto";
import { and, eq } from "drizzle-orm";
import { cookies, headers } from "next/headers";
import { NextResponse } from "next/server";

import { auth } from "@/lib/auth";
import { db } from "@/db/client";
import { sessions } from "@/db/schema";

/**
 * Web (NextAuth) sessions feed — the "this device" row (#685 follow-up,
 * TC-09 watch-item).
 *
 * /account/sessions previously listed ONLY identity-svc AuthService
 * sessions, which are empty for NextAuth-authenticated users (web
 * sessions live in the Drizzle `session` table — the same architecture
 * divergence #685 fixed for identifiers). This route surfaces the web's
 * own session rows so the panel can always show at least the current
 * browser session.
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

export async function GET(req: Request) {
  const session = await auth();
  const userId = session?.user?.id;
  if (!userId || !db) {
    return NextResponse.json({ sessions: [] }, { status: userId ? 503 : 401 });
  }
  const rows = await db
    .select({ sessionToken: sessions.sessionToken, expires: sessions.expires })
    .from(sessions)
    .where(eq(sessions.userId, userId));

  const current = tokenHash(await currentToken());
  const ua = (await headers()).get("user-agent") ?? "";

  return NextResponse.json({
    sessions: rows.map((r) => {
      const id = tokenHash(r.sessionToken);
      const isCurrent = id === current;
      return {
        id,
        is_current: isCurrent,
        // The NextAuth session table stores no UA/IP; the only UA we
        // can truthfully attribute is the current request's own.
        user_agent: isCurrent ? ua : "",
        expires_at: r.expires.toISOString(),
        kind: "web",
      };
    }),
  });
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
  if (id === tokenHash(await currentToken())) {
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
