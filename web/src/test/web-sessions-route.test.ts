import { describe, expect, it, vi, beforeEach } from "vitest";
import { createHash } from "node:crypto";

/**
 * Tests for the /account/sessions/web-sessions route — the "this device"
 * row feed (Refs #685, #693, TC-09). This route is the reason the current
 * browser session renders at all: NextAuth sessions live in the Drizzle
 * `session` table and never reach identity-svc, so without this feed the
 * panel had no current row to pin. The security-sensitive bits — current-
 * session detection by token-hash, UA attributed ONLY to the current row,
 * raw tokens NEVER returned, and the current session being un-revokable —
 * were untested. Pinned here.
 *
 * `auth`, `db`, and `next/headers` are mocked (same approach as
 * bff-proxy.test.ts for `auth`).
 */

vi.mock("@/lib/auth", () => ({ auth: vi.fn() }));
vi.mock("@/db/client", () => ({ db: { select: vi.fn(), delete: vi.fn() } }));
vi.mock("next/headers", () => ({ cookies: vi.fn(), headers: vi.fn() }));

import { auth } from "@/lib/auth";
import { db } from "@/db/client";
import { cookies, headers } from "next/headers";
import { GET, DELETE } from "@/app/account/sessions/web-sessions/route";

const hash = (t: string) =>
  createHash("sha256").update(t).digest("hex").slice(0, 16);

type Row = { sessionToken: string; expires: Date };

function setAuth(userId: string | null) {
  (auth as unknown as { mockResolvedValue: (v: unknown) => void }).mockResolvedValue(
    userId ? { user: { id: userId } } : null,
  );
}
function setCurrentCookie(token: string) {
  (cookies as unknown as { mockResolvedValue: (v: unknown) => void }).mockResolvedValue({
    get: (k: string) => (k.includes("session-token") ? { value: token } : undefined),
  });
}
function setUA(ua: string) {
  (headers as unknown as { mockResolvedValue: (v: unknown) => void }).mockResolvedValue({
    get: (k: string) => (k === "user-agent" ? ua : null),
  });
}
function setRows(rows: Row[]) {
  (db as unknown as { select: unknown }).select = vi.fn(() => ({
    from: () => ({ where: () => Promise.resolve(rows) }),
  }));
}

describe("web-sessions route — the TC-09 'this device' feed (#685/#693)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setUA("TestUA/1.0");
  });

  it("flags is_current=true ONLY on the session whose token-hash matches the current cookie", async () => {
    setAuth("user-1");
    setCurrentCookie("tokenA");
    setRows([
      { sessionToken: "tokenA", expires: new Date("2030-01-01") },
      { sessionToken: "tokenB", expires: new Date("2030-01-01") },
    ]);
    const res = await GET(new Request("http://x/account/sessions/web-sessions"));
    const body = (await res.json()) as { sessions: Array<Record<string, unknown>> };
    const a = body.sessions.find((s) => s.id === hash("tokenA"));
    const b = body.sessions.find((s) => s.id === hash("tokenB"));
    expect(a?.is_current).toBe(true);
    expect(b?.is_current).toBe(false);
    expect(a?.kind).toBe("web");
  });

  it("attributes the UA only to the current row, and NEVER returns the raw token", async () => {
    setAuth("user-1");
    setCurrentCookie("tokenA");
    setRows([
      { sessionToken: "tokenA", expires: new Date("2030-01-01") },
      { sessionToken: "tokenB", expires: new Date("2030-01-01") },
    ]);
    const body = (await (await GET(new Request("http://x"))).json()) as {
      sessions: Array<Record<string, unknown>>;
    };
    const current = body.sessions.find((s) => s.is_current);
    const other = body.sessions.find((s) => !s.is_current);
    expect(current?.user_agent).toBe("TestUA/1.0");
    expect(other?.user_agent).toBe("");
    // raw session tokens must never leave the server — every id is a hash
    for (const s of body.sessions) {
      expect(String(s.id)).toMatch(/^[0-9a-f]{16}$/);
      expect(s.id).not.toBe("tokenA");
      expect(s.id).not.toBe("tokenB");
    }
  });

  it("401s with an empty session list when unauthenticated", async () => {
    setAuth(null);
    const res = await GET(new Request("http://x"));
    expect(res.status).toBe(401);
    expect((await res.json()).sessions).toEqual([]);
  });

  it("DELETE refuses to revoke the CURRENT session (409 — sign out instead)", async () => {
    setAuth("user-1");
    setCurrentCookie("tokenA");
    const res = await DELETE(
      new Request(`http://x/account/sessions/web-sessions?id=${hash("tokenA")}`, {
        method: "DELETE",
      }),
    );
    expect(res.status).toBe(409);
  });

  it("DELETE requires an id param (400)", async () => {
    setAuth("user-1");
    setCurrentCookie("tokenA");
    const res = await DELETE(
      new Request("http://x/account/sessions/web-sessions", { method: "DELETE" }),
    );
    expect(res.status).toBe(400);
  });
});
