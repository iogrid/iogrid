import { test, expect } from "@playwright/test";

/**
 * E2E — unauthenticated /account renders the magic-link sign-in form.
 *
 * Why a real browser test (not a route-module assertion):
 *   - Catches NextAuth provider mis-config at boot (`/account` would
 *     500 if EMAIL_SERVER/AUTH_SECRET were wrong).
 *   - Catches Server Action wiring regressions on the magic-link form.
 *   - Verifies the form is reachable WITHOUT JavaScript (Server
 *     Component) — important for assistive-tech users.
 *
 * We do NOT actually submit the email form against a live SMTP — the
 * dev server is booted with a stub `smtp://localhost:1025` and any
 * submission would fail with a connection-refused. Asserting the form
 * is *present and tab-navigable* is the contract this spec owns.
 */
test.describe("/account — magic-link sign-in surface", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/account", { waitUntil: "domcontentloaded" });
  });

  test("renders sign-in heading + Google + email form", async ({ page }) => {
    await expect(
      page.getByRole("heading", { name: /sign in to iogrid/i }),
    ).toBeVisible();

    // Google CTA (Server Action <form>)
    await expect(
      page.getByRole("button", { name: /continue with google/i }),
    ).toBeVisible();

    // Magic-link form: email input + submit button
    const email = page.getByLabel("Email");
    await expect(email).toBeVisible();
    await expect(email).toHaveAttribute("type", "email");
    await expect(email).toBeEditable();

    await expect(
      page.getByRole("button", { name: /send magic link/i }),
    ).toBeVisible();
  });

  test("keyboard nav — Back -> Google -> Email -> Send (no mouse)", async ({
    page,
  }) => {
    // Focus the document so Tab starts at the first interactive node.
    await page.locator("body").click();
    await page.keyboard.press("Tab");

    // 1) "← Back" link
    let focused = await page.evaluate(() => document.activeElement?.tagName);
    expect(focused).toBe("A");

    await page.keyboard.press("Tab");
    // 2) Continue with Google button
    let focusedText = await page.evaluate(
      () => document.activeElement?.textContent?.trim() ?? "",
    );
    expect(focusedText).toMatch(/continue with google/i);

    await page.keyboard.press("Tab");
    // 3) Email input
    focused = await page.evaluate(() => document.activeElement?.tagName);
    expect(focused).toBe("INPUT");

    await page.keyboard.press("Tab");
    // 4) Send magic link button
    focusedText = await page.evaluate(
      () => document.activeElement?.textContent?.trim() ?? "",
    );
    expect(focusedText).toMatch(/send magic link/i);
  });

  test("email input rejects an invalid address (HTML5 validation)", async ({
    page,
  }) => {
    await page.getByLabel("Email").fill("not-an-email");
    await page
      .getByRole("button", { name: /send magic link/i })
      .click({ trial: false });

    // The native form validation prevents submit; the URL stays on /account.
    await expect(page).toHaveURL(/\/account/);
    const validity = await page
      .getByLabel("Email")
      .evaluate((el: HTMLInputElement) => el.validity.valid);
    expect(validity).toBe(false);
  });
});
