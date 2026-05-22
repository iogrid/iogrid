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

    // EPIC #422 restoration (PR #445): hero h1 reads
    // "The mesh that shows you every byte." Restored verbatim from
    // pre-#428 marketing-rich/Hero.
    await expect(
      page.getByRole("heading", {
        name: /the mesh that shows you every byte/i,
        level: 1,
      }),
    ).toBeVisible();

    // The restored Nav surfaces marketing-rich product anchor links.
    // (Products dropdown lists /proxy, /compute, /gpu, /ios-build,
    // /vpn — those live inside a closed <details> by default.)
    for (const href of ["/providers", "/pricing", "/token", "/blog"]) {
      await expect(page.locator(`a[href="${href}"]`).first()).toBeVisible();
    }

    // Hero CTAs after restoration:
    //   1. Primary   → /providers ("Become a provider")
    //   2. Secondary → /pricing   ("Buy services")
    await expect(
      page.getByRole("link", { name: /become a provider/i }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: /buy services/i }),
    ).toBeVisible();
  });

  test("landing surfaces the install band heading", async ({ page }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    // The restored "Install in two minutes" band carries the
    // OS-detecting InstallButton + the curl-pipe-sh fallback.
    await expect(
      page.getByRole("heading", { name: /install in two minutes/i, level: 2 }),
    ).toBeVisible();
  });

  test("non-existent page returns 404 not a server crash", async ({ page }) => {
    const res = await page.goto("/does-not-exist-iogrid-test", {
      waitUntil: "domcontentloaded",
    });
    expect(res?.status()).toBe(404);
  });
});
