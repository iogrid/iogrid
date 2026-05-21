import { test, expect } from "@playwright/test";

/**
 * E2E — same-origin BFF proxy routes are wired and gate anon (#237).
 *
 * The bug: every browser fetch from `app.iogrid.org` to
 * `api.iogrid.org/api/v1/*` (provider dashboard, earnings, admin
 * queues, ListProviders, ...) returned HTTP 401 because the web uses
 * NextAuth (cookies) and gateway-bff requires an identity-svc Bearer
 * JWT — no bridge existed.
 *
 * The fix: every cross-origin call was migrated to a same-origin
 * Next.js Route Handler under `/api/v1/*` that reads the session
 * server-side, then forwards to gateway-bff with the shared
 * IOGRID_SERVICE_TOKEN + X-Iogrid-User-Id shim.
 *
 * What we can verify on CI (no live gateway-bff, no AUTH_TRUST_HOST):
 *
 *   1. The Route Handler exists — i.e. the path returns a JSON 401
 *      response (and NOT a 404). Pre-fix this path did not exist on
 *      the web origin at all.
 *   2. The Route Handler refuses to proxy without a session — i.e.
 *      anon hits 401 BEFORE any upstream fetch (proving the auth()
 *      gate runs first).
 *   3. The 401 envelope matches the documented `{code,message}` shape
 *      so the ApiClient error parser sees what it expects.
 *
 * The "authed → 200" contract is covered by the bff-proxy unit test
 * (web/src/test/bff-proxy.test.ts) where we can mock `auth()`
 * directly. Asserting the cross-origin contract end-to-end requires
 * a real signed session AND a trusted host AND a live gateway-bff,
 * which lives in the post-deploy smoke set (PHASE0-UNBLOCK step 4d).
 */

// Admin BFF routes (/api/v1/admin/*) moved to the independent admin/
// app on admin.iogrid.org in EPIC #422 Phase 1 — they're no longer
// hosted by web/, so they're not asserted here. The admin app gets its
// own e2e suite in a follow-up (admin/tests/e2e/cross-origin-bridge).
const PROXY_PATHS = [
  { path: "/api/v1/provide/dashboard", method: "GET" as const },
  { path: "/api/v1/provide/schedule", method: "GET" as const },
  { path: "/api/v1/provide/earnings", method: "GET" as const },
  { path: "/api/v1/provide/audit/stream", method: "GET" as const },
];

test.describe("cross-origin bridge — same-origin BFF proxy (#237)", () => {
  for (const { path, method } of PROXY_PATHS) {
    test(`anon ${method} ${path} returns 401 (route is wired, gates anon)`, async ({
      page,
    }) => {
      const resp =
        method === "GET"
          ? await page.request.get(path)
          : await page.request.post(path, { data: {} });

      // Pre-fix: this path didn't exist on the web origin at all
      // (the browser dialled api.iogrid.org cross-origin instead and
      // hit the BFF's 401). A 404 here would mean the Route Handler
      // wasn't registered — regression.
      expect(
        resp.status(),
        `${method} ${path} must NOT be 404 — Route Handler missing`,
      ).not.toBe(404);

      // Anon must be rejected by the route handler itself, NOT
      // forwarded upstream. The status MUST be 401.
      expect(resp.status()).toBe(401);

      const body = await resp.json().catch(() => null);
      if (body) {
        expect(body.code).toBe("unauthenticated");
      }
    });
  }

  // Admin BFF routes (/api/v1/admin/*) live in the independent admin/
  // app now (EPIC #422 Phase 1). These two assertions are kept as
  // 404-on-web checks — they MUST 404 here to prove the routes were
  // actually moved off the user-facing host.
  test("/api/v1/admin/abuse/:id/resolve returns 404 on web (moved to admin/)", async ({
    page,
  }) => {
    const resp = await page.request.post(
      "/api/v1/admin/abuse/00000000-0000-0000-0000-000000000000/resolve",
      { data: { decision: "allow" } },
    );
    expect(resp.status()).toBe(404);
  });

  test("/api/v1/admin/providers/list returns 404 on web (moved to admin/)", async ({
    page,
  }) => {
    const resp = await page.request.post("/api/v1/admin/providers/list", {
      data: {},
    });
    expect(resp.status()).toBe(404);
  });
});
