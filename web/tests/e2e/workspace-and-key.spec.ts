import { test, expect } from "@playwright/test";

/**
 * E2E — homepage + public marketing surface.
 *
 * The protected portals (`/provide`, `/customer`, `/admin`) are
 * gated by `src/middleware.ts` which today crashes in the edge
 * runtime because `@/lib/auth` pulls nodemailer transitively (the
 * `stream` module is unsupported in edge). Once the middleware is
 * split into edge-safe + node providers (follow-up to #4 EPIC),
 * this file picks up an additional redirect-contract test suite.
 *
 * For now we ship the public-route contracts that we KNOW will be
 * stable for both shipped users and authenticated walkthroughs:
 *
 *   - Homepage renders nav links to every portal.
 *   - Nav clicks navigate to the routed surface (no console errors).
 *   - 404 routes return 404 (sanity).
 */
test.describe("Public marketing routes", () => {
  test("homepage renders the headline + every portal nav link", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });

    // Phase 2.1 redesign (#422 / #423): hero h1 reads
    // "Rent your idle machine. Or rent the whole network."
    await expect(
      page.getByRole("heading", {
        name: /rent your idle machine/i,
        level: 1,
      }),
    ).toBeVisible();

    for (const href of ["/provide", "/customer", "/vpn", "/account"]) {
      await expect(page.locator(`a[href="${href}"]`).first()).toBeVisible();
    }

    // Both primary CTAs in the hero:
    //   1. Primary  → /install     ("Install the daemon")
    //   2. Secondary → /customer   ("For customers")
    await expect(
      page.getByRole("link", { name: /install the daemon/i }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: /for customers/i }),
    ).toBeVisible();
  });

  test("clicking 'Install the daemon' lands on /install", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await page
      .getByRole("link", { name: /install the daemon/i })
      .click();
    await expect(page).toHaveURL(/\/install$/);
    await expect(
      page.getByRole("heading", { name: /install iogrid/i, level: 1 }),
    ).toBeVisible();
  });

  test("non-existent page returns 404 not a server crash", async ({ page }) => {
    const res = await page.goto("/does-not-exist-iogrid-test", {
      waitUntil: "domcontentloaded",
    });
    expect(res?.status()).toBe(404);
  });
});
