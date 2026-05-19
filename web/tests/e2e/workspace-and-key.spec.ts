import { test, expect } from "@playwright/test";

/**
 * E2E — customer surface: workspace overview + API-key page navigation.
 *
 * The /customer routes do NOT require an authenticated NextAuth
 * session at the moment (they live behind workspace-id local storage
 * for the BFF, but the page chrome itself is publicly renderable for
 * marketing reasons). When auth is later added (#3 EPIC), this spec
 * gains a session-cookie injection step before navigation.
 *
 * Today we assert:
 *   - /customer renders the workspace badge + nav links.
 *   - The "API keys" nav entry routes to /customer/api-keys.
 *   - The API-keys panel shows the "select workspace first" empty
 *     state when no workspace is in localStorage (the canonical
 *     un-onboarded experience).
 */
test.describe("Customer workspace surface", () => {
  test("/customer overview renders the portal shell", async ({ page }) => {
    await page.goto("/customer", { waitUntil: "domcontentloaded" });

    // Portal badge: small uppercase label above the page title.
    await expect(page.getByText(/^Customer$/)).toBeVisible();

    // Page heading.
    await expect(
      page.getByRole("heading", { name: /workspace/i, level: 1 }),
    ).toBeVisible();

    // Nav must include every customer sub-route.
    const expectedNav = [
      { name: /overview/i, href: "/customer" },
      { name: /workloads/i, href: "/customer/workloads" },
      { name: /api keys/i, href: "/customer/api-keys" },
      { name: /usage/i, href: "/customer/usage" },
      { name: /billing/i, href: "/customer/billing" },
    ];
    for (const item of expectedNav) {
      const link = page.getByRole("link", { name: item.name }).first();
      await expect(link).toBeVisible();
      await expect(link).toHaveAttribute("href", item.href);
    }
  });

  test("nav click routes /customer -> /customer/api-keys", async ({ page }) => {
    await page.goto("/customer", { waitUntil: "domcontentloaded" });

    await page.getByRole("link", { name: /api keys/i }).first().click();

    await expect(page).toHaveURL(/\/customer\/api-keys/);
    await expect(
      page.getByRole("heading", { name: /api keys/i, level: 1 }),
    ).toBeVisible();
  });

  test("/customer/api-keys shows the workspace-selection empty state", async ({
    page,
  }) => {
    // Ensure no workspace is preselected.
    await page.addInitScript(() => {
      localStorage.removeItem("iogrid_workspace_id");
    });

    await page.goto("/customer/api-keys", { waitUntil: "domcontentloaded" });

    // The panel renders an amber callout when no workspace is in scope.
    const callout = page.getByText(
      /select a workspace on the\s+overview tab/i,
    );
    await expect(callout).toBeVisible();
  });

  test("provider portal — /provide renders Install CTA + provider nav", async ({
    page,
  }) => {
    await page.goto("/provide", { waitUntil: "domcontentloaded" });

    await expect(
      page.getByRole("heading", { name: /provider overview/i, level: 1 }),
    ).toBeVisible();

    await expect(
      page.getByRole("link", { name: /install the daemon/i }),
    ).toBeVisible();

    // Every provider sub-route must be reachable from the nav.
    for (const href of [
      "/provide",
      "/provide/earnings",
      "/provide/schedule",
      "/provide/staking",
      "/provide/audit",
    ]) {
      await expect(page.locator(`a[href="${href}"]`).first()).toBeVisible();
    }
  });
});
