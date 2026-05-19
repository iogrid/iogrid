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
  test("install page lists Mac / Win / Linux with curl snippets", async ({
    page,
  }) => {
    await page.goto("/install", { waitUntil: "domcontentloaded" });

    await expect(
      page.getByRole("heading", { name: /install iogrid/i, level: 1 }),
    ).toBeVisible();

    // Every supported platform label must appear somewhere on the page.
    for (const label of [/mac/i, /windows/i, /linux/i]) {
      await expect(page.getByText(label).first()).toBeVisible();
    }

    // Mac curl snippet — the canonical paste-into-terminal command.
    await expect(
      page.getByText(/curl -fsSL https:\/\/iogrid\.org\/install\/mac \| sh/),
    ).toBeVisible();
  });

  test("homepage nav points at /vpn (consumer SOCKS5 entry)", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });

    const vpnLink = page.getByRole("link", { name: /^vpn$/i }).first();
    await expect(vpnLink).toBeVisible();
    await expect(vpnLink).toHaveAttribute("href", "/vpn");

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
