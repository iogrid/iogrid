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
 * Acceptance:
 *   1. First load resolves to either "light" or "dark" via system
 *      preference (Playwright default is light).
 *   2. Clicking the toggle moves the html.class from "light" → "dark"
 *      (or vice-versa) AND updates the rendered <body> background
 *      colour.
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

    // Initial state: light (system preference is light).
    await expect(page.locator("html")).not.toHaveClass(/(^|\s)dark(\s|$)/);
    const lightBg = await page.evaluate(
      () => getComputedStyle(document.body).backgroundColor,
    );

    // First click: light → dark.
    await toggle.click();
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
    // light. We check the class on `<html>` immediately after the
    // navigation completes; next-themes' inline blocking script runs
    // before React hydrates, so the class is present in the first
    // painted frame.
    await page.reload();
    await expect(page.locator("html")).toHaveClass(/(^|\s)dark(\s|$)/);
  });

  test("cycles through system, dark, light on successive clicks", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/");
    // Start clean — no persisted choice from prior tests.
    await page.evaluate(() => localStorage.removeItem("iogrid-theme"));
    await page.reload();

    const toggle = page.getByRole("button", { name: /switch to .+ theme/i });
    await expect(toggle).toBeEnabled();

    // system → dark (the first click from default always produces a
    // visible change, regardless of system preference)
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "dark");

    // dark → light
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "light");

    // light → system (back to default; one more click would re-enter
    // the dark→light cycle)
    await toggle.click();
    await expect(toggle).toHaveAttribute("data-theme-toggle", "system");
  });
});
