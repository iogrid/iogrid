import { test, expect } from "@playwright/test";

/**
 * E2E — protected-route surface + customer/provider portal contract.
 *
 * The `/provide`, `/customer`, and `/admin` route prefixes are
 * protected by `src/middleware.ts`. Unauthenticated users get a 307
 * to `/account?callbackUrl=<original>`. The contract we own:
 *
 *   1. The middleware actually fires — visiting `/customer` without a
 *      session redirects to /account with callbackUrl preserved.
 *   2. /customer/api-keys (deeper path) also redirects.
 *   3. After login, the post-redirect contract is to land back on
 *      callbackUrl (not exercised here — we have no real OAuth
 *      session in CI, and stubbing NextAuth's JWT flow is out of
 *      scope for this PR).
 *
 * Real authenticated walkthroughs of the customer overview + API-key
 * issue flow live in the follow-up PR (#3 EPIC) once the
 * mock-session-cookie helper is added.
 */
test.describe("Protected portal surfaces — middleware redirects", () => {
  test("/customer redirects unauthenticated → /account?callbackUrl=/customer", async ({
    page,
  }) => {
    await page.goto("/customer", { waitUntil: "domcontentloaded" });

    await expect(page).toHaveURL(/\/account\?callbackUrl=%2Fcustomer$/);

    // Landed on the sign-in panel.
    await expect(
      page.getByRole("heading", { name: /sign in to iogrid/i }),
    ).toBeVisible();
  });

  test("/customer/api-keys deep-link preserves callbackUrl through redirect", async ({
    page,
  }) => {
    await page.goto("/customer/api-keys", { waitUntil: "domcontentloaded" });

    await expect(page).toHaveURL(
      /\/account\?callbackUrl=%2Fcustomer%2Fapi-keys$/,
    );
  });

  test("/provide redirects unauthenticated → /account?callbackUrl=/provide", async ({
    page,
  }) => {
    await page.goto("/provide", { waitUntil: "domcontentloaded" });

    await expect(page).toHaveURL(/\/account\?callbackUrl=%2Fprovide$/);
  });

  test("/admin redirects unauthenticated", async ({ page }) => {
    await page.goto("/admin", { waitUntil: "domcontentloaded" });
    await expect(page).toHaveURL(/\/account/);
  });
});

test.describe("Public marketing routes", () => {
  test("homepage renders nav links to every portal", async ({ page }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });

    await expect(
      page.getByRole("heading", {
        name: /distributed compute mesh/i,
        level: 1,
      }),
    ).toBeVisible();

    for (const href of ["/provide", "/customer", "/vpn", "/account"]) {
      await expect(page.locator(`a[href="${href}"]`).first()).toBeVisible();
    }
  });
});
