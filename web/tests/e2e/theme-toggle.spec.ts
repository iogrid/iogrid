import { test, expect } from "@playwright/test";

/**
 * E2E — theme toggle works end-to-end on the marketing landing page.
 *
 * Why /:
 *   - Anonymous route, no auth setup required.
 *   - Renders the ThemeToggle directly (no PortalShell auth roundtrip).
 *   - Lets us assert localStorage persistence + html.class flip
 *     without any backend dependency.
 *
 * Cycle: system → dark → light → system. First-time visitors land on
 * `theme === "system"`, so the FIRST click moves them to dark
 * regardless of OS preference — see `theme-toggle.tsx` for the why.
 *
 * Acceptance:
 *   1. First load resolves to system (no localStorage), html class
 *      tracks the resolved scheme.
 *   2. First click sets theme="dark" → html.dark + body background
 *      changes from the system-resolved colour.
 *   3. The choice persists across reload via `localStorage["iogrid-theme"]`.
 */

test.describe("theme toggle", () => {
  test("flips html.dark on click and persists in localStorage", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/");

    // Wait for next-themes to hydrate and the toggle to enable.
    const toggle = page.getByRole("button", { name: /switch to .+ theme/i });
    await expect(toggle).toBeEnabled();

    // Initial state: theme="system", resolves to light → html has
    // class "light" (not "dark").
    await expect(page.locator("html")).not.toHaveClass(/(^|\s)dark(\s|$)/);
    const lightBg = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor,
    );

    // First click: system → dark.
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "dark");
    await expect(page.locator("html")).toHaveClass(/(^|\s)dark(\s|$)/);

    const darkBg = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor,
    );
    expect(darkBg).not.toBe(lightBg);

    // localStorage carries the explicit choice.
    const stored = await page.evaluate(() =>
      localStorage.getItem("iogrid-theme"),
    );
    expect(stored).toBe("dark");

    // Reload — the dark choice should survive without a flash to
    // light. next-themes' inline blocking script runs before React
    // hydrates, so the class is present in the first painted frame.
    await page.reload();
    await expect(page.locator("html")).toHaveClass(/(^|\s)dark(\s|$)/);
  });

  test("cycles system → dark → light → system on successive clicks", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/");
    // Start clean — no persisted choice.
    await page.evaluate(() => localStorage.removeItem("iogrid-theme"));
    await page.reload();

    const toggle = page.getByRole("button", { name: /switch to .+ theme/i });
    await expect(toggle).toBeEnabled();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "system");

    // system → dark
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "dark");

    // dark → light
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "light");

    // light → system
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "system");
  });
});
