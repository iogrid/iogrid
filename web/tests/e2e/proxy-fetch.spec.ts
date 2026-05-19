import { test, expect } from "@playwright/test";

/**
 * E2E — /install page + the proxy.iogrid.org BFF contract.
 *
 * The actual SOCKS5 fetch test lives in the daemon test suite
 * (daemon/tests/socks5_proxy_test.rs); it needs a live VPN entry and
 * cannot run from inside a Playwright browser. What we *can* verify
 * from the web plane is:
 *
 *   1. /install lists every supported platform with a curl-pipe-sh
 *      snippet so customers can paste it into their terminal.
 *   2. /install lists signed-package URLs for non-curl users.
 *   3. /vpn (the consumer-VPN landing) is reachable from the homepage
 *      nav — the SOCKS5 entry point is documented there.
 *
 * If any of the platform CTAs go missing the proxy-fetch onboarding
 * flow breaks for end users; this spec is the canary.
 */
test.describe("/install + proxy onboarding surface", () => {
  test("install page lists Mac / Win / Linux platform sections", async ({
    page,
  }) => {
    await page.goto("/install", { waitUntil: "domcontentloaded" });

    await expect(
      page.getByRole("heading", { name: /install iogrid/i, level: 1 }),
    ).toBeVisible();

    // Each platform renders an h2 heading.
    for (const label of [/macos/i, /windows/i, /linux/i]) {
      await expect(
        page.getByRole("heading", { name: label, level: 2 }).first(),
      ).toBeVisible();
    }

    // Mac curl snippet — text is server-rendered inside a closed
    // <details> element. Expand the "Prefer the terminal?" summary
    // for the macOS section, then assert the snippet becomes visible.
    const macSection = page.locator("#mac");
    const macSummary = macSection.getByText(/prefer the terminal\?/i).first();
    await macSummary.click();
    await expect(
      macSection.getByText(
        /curl -fsSL https:\/\/iogrid\.org\/install\/mac \| sh/,
      ),
    ).toBeVisible();
  });

  test("install page lists signed-package download links", async ({ page }) => {
    await page.goto("/install", { waitUntil: "domcontentloaded" });

    // At least one .pkg, one .msi, and one .deb anchor exist.
    await expect(
      page.locator('a[href*=".pkg"]').first(),
    ).toBeVisible();
    await expect(
      page.locator('a[href*=".msi"]').first(),
    ).toBeVisible();
    await expect(
      page.locator('a[href*=".deb"]').first(),
    ).toBeVisible();
  });

  test("homepage nav points at /vpn (consumer SOCKS5 entry)", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });

    const vpnLink = page.locator('a[href="/vpn"]').first();
    await expect(vpnLink).toBeVisible();

    await vpnLink.click();
    await expect(page).toHaveURL(/\/vpn$/);
  });

  test("non-existent page returns a 404 not a server crash", async ({
    page,
  }) => {
    const res = await page.goto("/does-not-exist-iogrid-test", {
      waitUntil: "domcontentloaded",
    });
    expect(res?.status()).toBe(404);
  });
});
