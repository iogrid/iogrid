import { describe, expect, it, vi, beforeEach } from "vitest";
import { createHash } from "node:crypto";

/**
 * Tests for the /account/sessions/web-sessions route — the "this device"
 * row feed (Refs #685, #693, TC-09; root cause finished in #808).
 *
 * This route is the reason the current browser session renders at all:
 * NextAuth sessions never reach identity-svc, so without this feed the
 * panel has no current row to pin.
 *
 * #808 regression guard: the original implementation listed ONLY rows
 * from the Drizzle `session` table. But the app runs `session:{strategy:
 * "jwt"}` — under JWT strategy that table is ALWAYS empty (the session is
 * a signed cookie, not a DB row), so the route returned `[]` and NO "this
 * device" row ever rendered in prod. The first test below reproduces that
 * exact condition (authed session + EMPTY table + a session-token cookie)
 * and asserts a current-device row is STILL synthesized. The pre-#808
 * tests mocked a populated table and so never exercised the failing path.
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

const EXPIRES = "2030-01-01T00:00:00.000Z";

type Row = { sessionToken: string; expires: Date };

function setAuth(userId: string | null) {
  (
    auth as unknown as { mockResolvedValue: (v: unknown) => void }
  ).mockResolvedValue(userId ? { user: { id: userId }, expires: EXPIRES } : null);
}
function setCurrentCookie(token: string) {
  (
    cookies as unknown as { mockResolvedValue: (v: unknown) => void }
  ).mockResolvedValue({
    get: (k: string) =>
      k.includes("session-token") ? { value: token } : undefined,
  });
}
function setNoCookie() {
  (
    cookies as unknown as { mockResolvedValue: (v: unknown) => void }
  ).mockResolvedValue({ get: () => undefined });
}
function setUA(ua: string) {
  (
    headers as unknown as { mockResolvedValue: (v: unknown) => void }
  ).mockResolvedValue({
    get: (k: string) => (k === "user-agent" ? ua : null),
  });
}
function setRows(rows: Row[]) {
  (db as unknown as { select: unknown }).select = vi.fn(() => ({
    from: () => ({ where: () => Promise.resolve(rows) }),
  }));
}

describe("web-sessions route — the TC-09 'this device' feed (#685/#693/#808)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setUA("TestUA/1.0");
  });

  it("#808: synthesizes a current-device row even when the session table is EMPTY (JWT strategy)", async () => {
    // The prod condition: authed via a JWT cookie, Drizzle `session` table
    // empty (strategy:"jwt" never writes to it). Pre-#808 this returned []
    // and the panel showed "Unknown device · Expired".
    setAuth("user-1");
    setCurrentCookie("jwt-cookie");
    setRows([]);
    const res = await GET(new Request("http://x/account/sessions/web-sessions"));
    const body = (await res.json()) as {
      sessions: Array<Record<string, unknown>>;
    };
    expect(body.sessions).toHaveLength(1);
    const cur = body.sessions[0];
    expect(cur.is_current).toBe(true);
    expect(cur.user_agent).toBe("TestUA/1.0");
    expect(cur.expires_at).toBe(EXPIRES);
    expect(cur.kind).toBe("web");
    // id is the cookie hash (so DELETE-revocation hashing stays consistent),
    // never the raw token.
    expect(cur.id).toBe(hash("jwt-cookie"));
    expect(cur.id).not.toBe("jwt-cookie");
  });

  it("#808: still renders a current row if the session-token cookie is absent", async () => {
    setAuth("user-1");
    setNoCookie();
    setRows([]);
    const body = (await (await GET(new Request("http://x"))).json()) as {
      sessions: Array<Record<string, unknown>>;
    };
    expect(body.sessions).toHaveLength(1);
    expect(body.sessions[0].is_current).toBe(true);
    expect(body.sessions[0].id).toBe("current-device");
  });

  it("forward-compat: persisted (database-strategy) rows surface as non-current, current cookie never duplicated", async () => {
    setAuth("user-1");
    setCurrentCookie("tokenA");
    // tokenA is the current cookie AND also a persisted row → must appear
    // exactly once, as the synthesized current row (not twice).
    setRows([
      { sessionToken: "tokenA", expires: new Date("2030-01-01") },
      { sessionToken: "tokenB", expires: new Date("2030-01-01") },
    ]);
    const body = (await (await GET(new Request("http://x"))).json()) as {
      sessions: Array<Record<string, unknown>>;
    };
    const a = body.sessions.filter((s) => s.id === hash("tokenA"));
    const b = body.sessions.find((s) => s.id === hash("tokenB"));
    expect(a).toHaveLength(1);
    expect(a[0].is_current).toBe(true);
    expect(b?.is_current).toBe(false);
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
    // (or the fixed "current-device" sentinel).
    for (const s of body.sessions) {
      expect(String(s.id)).toMatch(/^([0-9a-f]{16}|current-device)$/);
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
      new Request(
        `http://x/account/sessions/web-sessions?id=${hash("tokenA")}`,
        { method: "DELETE" },
      ),
    );
    expect(res.status).toBe(409);
  });

  it("DELETE requires an id param (400)", async () => {
    setAuth("user-1");
    setCurrentCookie("tokenA");
    const res = await DELETE(
      new Request("http://x/account/sessions/web-sessions", {
        method: "DELETE",
      }),
    );
    expect(res.status).toBe(400);
  });
});
