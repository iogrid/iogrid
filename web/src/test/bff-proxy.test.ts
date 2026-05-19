import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

/**
 * Unit tests for `lib/bff-proxy` — the same-origin BFF proxy helper
 * that bridges NextAuth → gateway-bff for issue #237.
 *
 * We mock `@/lib/auth` so we can drive `auth()`'s return value per-
 * test; the rest is plain fetch stubbing on globalThis.
 */

vi.mock("@/lib/auth", () => ({
  auth: vi.fn(),
}));

import { auth } from "@/lib/auth";
import { proxyToBff } from "@/lib/bff-proxy";

type AuthFn = () => Promise<unknown>;

function setSession(user: unknown | null) {
  (auth as unknown as { mockResolvedValue: (v: unknown) => void }).mockResolvedValue(
    user ? { user } : null,
  );
}

describe("proxyToBff (#237)", () => {
  const realFetch = globalThis.fetch;
  let lastFetchArgs: { url: string; init: RequestInit } | null = null;

  beforeEach(() => {
    lastFetchArgs = null;
    process.env.IOGRID_GATEWAY_BFF_URL = "http://upstream.test:8080";
    process.env.IOGRID_SERVICE_TOKEN = "svc-secret";
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
    delete process.env.IOGRID_GATEWAY_BFF_URL;
    delete process.env.IOGRID_SERVICE_TOKEN;
    (auth as unknown as { mockReset: () => void }).mockReset();
  });

  /** Build a minimal NextRequest-shape object for unit testing. */
  function fakeReq(
    method: string,
    pathAndSearch: string,
    body?: string,
    headers: Record<string, string> = {},
  ): import("next/server").NextRequest {
    const url = `http://app.test${pathAndSearch}`;
    return {
      url,
      method,
      headers: {
        get: (k: string) =>
          headers[k.toLowerCase()] ?? headers[k] ?? null,
      },
      text: async () => body ?? "",
    } as unknown as import("next/server").NextRequest;
  }

  it("returns 401 when there is no session", async () => {
    setSession(null);
    const req = fakeReq("GET", "/api/v1/provide/earnings");
    const resp = await proxyToBff(req);
    expect(resp.status).toBe(401);
    const body = await resp.json();
    expect(body.code).toBe("unauthenticated");
  });

  it("returns 503 when IOGRID_SERVICE_TOKEN is unset (env not wired)", async () => {
    delete process.env.IOGRID_SERVICE_TOKEN;
    setSession({ id: "00000000-0000-0000-0000-000000000001" });
    const req = fakeReq("GET", "/api/v1/provide/earnings");
    const resp = await proxyToBff(req);
    expect(resp.status).toBe(503);
    const body = await resp.json();
    expect(body.code).toBe("bff_proxy_unavailable");
  });

  it("forwards GET with the service-token + X-Iogrid-User-Id headers", async () => {
    setSession({
      id: "00000000-0000-0000-0000-0000000000aa",
      email: "alice@example.com",
    });
    globalThis.fetch = (async (url: string, init: RequestInit) => {
      lastFetchArgs = { url, init };
      return new Response(JSON.stringify({ summary: { total: "12.34" } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }) as typeof fetch;

    const req = fakeReq("GET", "/api/v1/provide/earnings?start=2026-01-01");
    const resp = await proxyToBff(req);
    expect(resp.status).toBe(200);
    const body = await resp.json();
    expect(body.summary.total).toBe("12.34");

    expect(lastFetchArgs).not.toBeNull();
    expect(lastFetchArgs!.url).toBe(
      "http://upstream.test:8080/api/v1/provide/earnings?start=2026-01-01",
    );
    const outHeaders = lastFetchArgs!.init.headers as Record<string, string>;
    expect(outHeaders["authorization"]).toBe("Bearer svc-secret");
    expect(outHeaders["x-iogrid-user-id"]).toBe(
      "00000000-0000-0000-0000-0000000000aa",
    );
    expect(outHeaders["x-iogrid-user-email"]).toBe("alice@example.com");
  });

  it("merges session roles + extraRoles into X-Iogrid-User-Roles", async () => {
    setSession({
      id: "00000000-0000-0000-0000-0000000000bb",
      email: "ops@iogrid.org",
      roles: ["USER"],
    });
    globalThis.fetch = (async (url: string, init: RequestInit) => {
      lastFetchArgs = { url, init };
      return new Response(JSON.stringify({ rules: [] }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }) as typeof fetch;

    const req = fakeReq("GET", "/api/v1/admin/abuse-queue");
    const resp = await proxyToBff(req, { extraRoles: ["ADMIN"] });
    expect(resp.status).toBe(200);
    const outHeaders = lastFetchArgs!.init.headers as Record<string, string>;
    const roles = (outHeaders["x-iogrid-user-roles"] ?? "").split(",");
    expect(roles).toContain("USER");
    expect(roles).toContain("ADMIN");
  });

  it("forwards POST with the request body buffered", async () => {
    setSession({ id: "00000000-0000-0000-0000-0000000000cc" });
    globalThis.fetch = (async (url: string, init: RequestInit) => {
      lastFetchArgs = { url, init };
      return new Response(JSON.stringify({ accepted: true }), { status: 202 });
    }) as typeof fetch;
    const req = fakeReq(
      "POST",
      "/api/v1/provide/schedule",
      JSON.stringify({ config: { cpuCapPercent: 30 } }),
      { "content-type": "application/json" },
    );
    const resp = await proxyToBff(req);
    expect(resp.status).toBe(202);
    expect(lastFetchArgs!.init.method).toBe("POST");
    expect(lastFetchArgs!.init.body).toBe(
      JSON.stringify({ config: { cpuCapPercent: 30 } }),
    );
  });

  it("returns 502 when the upstream fetch throws", async () => {
    setSession({ id: "00000000-0000-0000-0000-0000000000dd" });
    globalThis.fetch = (async () => {
      throw new Error("connect ECONNREFUSED 127.0.0.1:8080");
    }) as typeof fetch;
    const req = fakeReq("GET", "/api/v1/provide/dashboard");
    const resp = await proxyToBff(req);
    expect(resp.status).toBe(502);
    const body = await resp.json();
    expect(body.code).toBe("bff_unreachable");
  });

  // Silence "unused variable" eslint warning when only side-effects matter.
  void (auth as unknown as AuthFn);
});
