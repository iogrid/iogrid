import { test, expect } from "@playwright/test";

/**
 * E2E — header-mounted PersonaSwitcher dropdown (#470).
 *
 * Why this matters: the post-#422 AppShell uses a single-pane layout
 * with the persona switcher in the top header. Founder picked this
 * shape over the prior dual-left-pane (icon rail + persona sidebar)
 * after seeing ASCII wireframes.
 *
 * The auth-protected /provider page is the natural place to verify
 * the dropdown because that's where AppShell renders. We use the
 * sign-in roundtrip from `tests/auth.setup.ts` (NextAuth magic-link
 * stub) so the dropdown actually mounts.
 *
 * Acceptance:
 *   1. Dropdown trigger button is visible in the header with the
 *      current persona's label.
 *   2. Clicking opens a menu listing all 4 personas + Sign out.
 *   3. Active persona row has the indicator dot + accent text.
 *   4. Clicking another persona navigates to /<persona>.
 *   5. Escape closes the menu.
 *
 * Tests are intentionally narrow — the broader vitest unit suite at
 * `web/src/test/persona-switcher.test.tsx` covers all 7 dropdown
 * states. This spec only proves the dropdown actually mounts on the
 * real /provider page (i.e. the AppShell + auth + persona-aware
 * route resolution all line up end-to-end).
 */

test.describe("PersonaSwitcher (#470) on /provider", () => {
  // Reuse the magic-link session set up in tests/auth.setup.ts so we
  // land on a signed-in /provider page that mounts AppShell.
  test.use({ storageState: "tests/.auth/user.json" });

  test("dropdown trigger + 4 personas + sign-out appear", async ({ page }) => {
    await page.goto("/provider");

    // Trigger is in the header.
    const trigger = page.getByTestId("persona-switcher-trigger");
    await expect(trigger).toBeVisible();
    await expect(trigger).toContainText("Provider");
    await expect(trigger).toHaveAttribute("aria-expanded", "false");

    await trigger.click();
    await expect(trigger).toHaveAttribute("aria-expanded", "true");

    // All 4 persona links rendered with their hrefs.
    await expect(page.getByTestId("persona-switch-provider")).toHaveAttribute(
      "href",
      "/provider",
    );
    await expect(page.getByTestId("persona-switch-customer")).toHaveAttribute(
      "href",
      "/customer",
    );
    await expect(page.getByTestId("persona-switch-vpn")).toHaveAttribute(
      "href",
      "/vpn",
    );
    await expect(page.getByTestId("persona-switch-account")).toHaveAttribute(
      "href",
      "/account",
    );
    await expect(page.getByTestId("persona-switcher-signout")).toBeVisible();
  });

  test("Escape closes the menu", async ({ page }) => {
    await page.goto("/provider");
    await page.getByTestId("persona-switcher-trigger").click();
    await expect(
      page.getByTestId("persona-switcher-trigger"),
    ).toHaveAttribute("aria-expanded", "true");

    await page.keyboard.press("Escape");
    await expect(
      page.getByTestId("persona-switcher-trigger"),
    ).toHaveAttribute("aria-expanded", "false");
  });

  test("click on a persona navigates to that route", async ({ page }) => {
    await page.goto("/provider");
    await page.getByTestId("persona-switcher-trigger").click();
    await page.getByTestId("persona-switch-customer").click();
    await expect(page).toHaveURL(/\/customer(\?|$)/);
  });
});
