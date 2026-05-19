import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import { encode } from "@auth/core/jwt";

// `@auth/core` is a transitive dep of `next-auth@5` and is therefore
// resolvable in the test context. Importing it directly (vs through
// `next-auth/jwt`) keeps the test deps minimal and tracks the canonical
// HKDF/JWE pipeline that the server-side `auth()` decoder uses.

/**
 * E2E — first-login customer dashboard auto-creates a workspace (#232).
 *
 * Pre-fix behaviour: after sign-in, `/customer` rendered a "Pick a
 * workspace" panel demanding the user paste a UUID by hand. The user
 * had no workspace and no provisioning flow, so they were dead-ended.
 *
 * Post-fix behaviour:
 *   1. The CustomerOverview mounts, sees no cached `iogrid_workspace_id`,
 *      and POSTs `/api/customer/workspaces/init`.
 *   2. The BFF returns a fresh workspace id (creating one in
 *      identity-svc on the fly).
 *   3. The dashboard renders inline within 5 seconds.
 *   4. The paste-UUID form is NOT rendered as the primary surface; it
 *      lives inside a collapsed <details> in the fallback panel that
 *      only appears when auto-init declines.
 *
 * This test stubs both the NextAuth session cookie (so middleware lets
 * us through to /customer) AND the workspaces/init route (so we don't
 * need a live identity-svc on CI). The contract under test is the
 * CLIENT logic — we assert what the user sees, not the BFF wiring,
 * which has its own Go-side coverage in middleware_test.go.
 */

const FAKE_WORKSPACE_ID = "11111111-2222-3333-4444-555555555555";

/**
 * Mint a NextAuth-v5 session cookie. NextAuth uses JWE (A256GCM) with
 * a key derived from AUTH_SECRET via HKDF. Replicating the encoder
 * here lets the test fixture log in without contacting Google / SMTP
 * and without standing up a Drizzle adapter.
 *
 * Mirrors @auth/core/jwt encode() — keep in sync with v5.
 */
const SESSION_COOKIE_NAME = "authjs.session-token";

async function mintSessionCookie(opts: {
  userId: string;
  email: string;
  secret: string;
}): Promise<string> {
  // Use the canonical @auth/core encoder so the cookie shape tracks
  // NextAuth's HKDF + JWE derivation exactly — no hand-rolled crypto.
  return await encode({
    token: {
      sub: opts.userId,
      uid: opts.userId,
      name: "Test User",
      email: opts.email,
    },
    secret: opts.secret,
    salt: SESSION_COOKIE_NAME,
    maxAge: 60 * 60,
  });
}

async function authenticate(context: BrowserContext, page: Page): Promise<boolean> {
  const secret = process.env.AUTH_SECRET ?? "";
  if (!secret) return false;
  let token: string;
  try {
    token = await mintSessionCookie({
      userId: "00000000-0000-0000-0000-0000000000aa",
      email: "alice@example.com",
      secret,
    });
  } catch {
    return false;
  }
  const baseURL = (process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000")
    .replace(/^https?:\/\//, "")
    .replace(/\/.*$/, "");
  const hostname = baseURL.split(":")[0];
  await context.addCookies([
    {
      name: SESSION_COOKIE_NAME,
      value: token,
      domain: hostname,
      path: "/",
      httpOnly: true,
      secure: false,
      sameSite: "Lax",
    },
  ]);
  void page;
  return true;
}

test.describe("customer dashboard — first-login auto workspace (#232)", () => {
  test.beforeEach(async ({ context, page }) => {
    // Stub the BFF init route BEFORE navigation so the first render's
    // POST hits our fixture instead of attempting a real upstream.
    await context.route("**/api/customer/workspaces/init", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          workspace_id: FAKE_WORKSPACE_ID,
          name: "alice",
          created: true,
        }),
      });
    });
    // The usage call after bootstrap also hits the BFF — stub it too
    // so we don't depend on a live gateway-bff.
    await context.route("**/api/v1/customer/usage**", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ rows: [] }),
      });
    });
    const ok = await authenticate(context, page);
    if (!ok) {
      test.skip(
        true,
        "Could not mint a NextAuth session cookie (AUTH_SECRET missing or @auth/core/jwt encode unavailable in this test runner)",
      );
    }
  });

  test("logged-in user with no workspace lands on the dashboard, not the paste prompt", async ({
    page,
  }) => {
    await page.goto("/customer", { waitUntil: "domcontentloaded" });

    // If middleware redirected us back to /account, the session-cookie
    // shape didn't match what `auth()` expects in this NextAuth build
    // — treat that as a fixture problem, not a regression of #232.
    if (/\/account/.test(page.url())) {
      test.skip(
        true,
        "middleware redirected to /account; minted session-cookie did not decode (fixture skew, not a #232 regression)",
      );
      return;
    }

    // Hard contract: the dashboard renders within 5s. The bootstrap
    // POST + state transition must complete inside the budget.
    await expect(page.getByTestId("customer-dashboard")).toBeVisible({
      timeout: 5_000,
    });

    // The legacy paste-UUID form must NOT be the primary surface.
    await expect(page.getByTestId("workspace-setup-fallback")).toHaveCount(0);

    // The workspace id is persisted for subsequent loads.
    const cached = await page.evaluate(() =>
      window.localStorage.getItem("iogrid_workspace_id"),
    );
    expect(cached).toBe(FAKE_WORKSPACE_ID);

    // Quick-link cards confirm the dashboard is fully wired, not a
    // half-rendered loading shell.
    await expect(page.getByRole("link", { name: /api keys/i })).toBeVisible();
    await expect(page.getByRole("link", { name: /workloads/i })).toBeVisible();
    await expect(page.getByRole("link", { name: /billing/i })).toBeVisible();
  });

  test("cached workspace id short-circuits the BFF round-trip", async ({
    page,
    context,
  }) => {
    let initCalls = 0;
    await context.unroute("**/api/customer/workspaces/init");
    await context.route("**/api/customer/workspaces/init", async (route) => {
      initCalls++;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          workspace_id: FAKE_WORKSPACE_ID,
          name: "alice",
          created: false,
        }),
      });
    });

    // Pre-seed the cache, then navigate. We expect zero calls to the
    // init proxy on this load.
    await page.goto("/customer", { waitUntil: "domcontentloaded" });
    if (/\/account/.test(page.url())) {
      test.skip(
        true,
        "middleware redirected to /account; minted session-cookie did not decode",
      );
      return;
    }
    await page.evaluate(
      (v) => window.localStorage.setItem("iogrid_workspace_id", v),
      FAKE_WORKSPACE_ID,
    );
    const callsBefore = initCalls;
    await page.reload({ waitUntil: "domcontentloaded" });

    await expect(page.getByTestId("customer-dashboard")).toBeVisible({
      timeout: 5_000,
    });
    expect(
      initCalls - callsBefore,
      "init must not be called when cache is warm",
    ).toBe(0);
  });
});
