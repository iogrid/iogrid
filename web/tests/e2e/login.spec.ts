import { test, expect } from "@playwright/test";

/**
 * E2E — unauthenticated /account renders the magic-link sign-in form.
 *
 * Why a real browser test (not a route-module assertion):
 *   - Catches NextAuth provider mis-config at boot (`/account` would
 *     500 if EMAIL_SERVER/AUTH_SECRET were wrong).
 *   - Catches Server Action wiring regressions on the magic-link form.
 *   - Verifies the form is reachable from a keyboard with no mouse.
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

  test("renders sign-in heading + magic-link form (Google gated by config)", async ({
    page,
  }) => {
    await expect(
      page.getByRole("heading", { name: /sign in to iogrid/i }),
    ).toBeVisible();

    // Magic-link form: the always-available auth path — email input + submit.
    const email = page.getByLabel("Email");
    await expect(email).toBeVisible();
    await expect(email).toHaveAttribute("type", "email");
    await expect(email).toBeEditable();

    await expect(
      page.getByRole("button", { name: /send magic link/i }),
    ).toBeVisible();

    // Google CTA is gated on a real OAuth client (#653 / #646): when
    // GOOGLE_CLIENT_ID is unset or the `phase0-placeholder` seed — which is
    // the case in CI — `googleSignInEnabled()` is false and the button is
    // intentionally hidden. Assert its visibility MATCHES the config rather
    // than hard-requiring presence (the unconditional assert broke the E2E
    // gate, #671).
    const clientId = process.env.GOOGLE_CLIENT_ID ?? "";
    const googleEnabled =
      clientId.length > 0 &&
      !clientId.toLowerCase().includes("phase0-placeholder") &&
      clientId.toLowerCase() !== "placeholder";
    const googleButton = page.getByRole("button", {
      name: /continue with google/i,
    });
    if (googleEnabled) {
      await expect(googleButton).toBeVisible();
    } else {
      await expect(googleButton).toHaveCount(0);
    }
  });

  test("keyboard nav — every form control is reachable by Tab", async ({
    page,
  }) => {
    // Focus the email field by tabbing forward — proves keyboard users
    // can reach it without a mouse. We don't assert the *exact* tab
    // index of each control (it depends on whether Chromium counts the
    // body link, focusable scrollers, etc.) — we assert the contract
    // that matters: the email input is reachable.
    const email = page.getByLabel("Email");
    const send = page.getByRole("button", { name: /send magic link/i });

    // Focus from the top of the document.
    await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());

    // Tab until the email input is focused (max 10 tabs — there are
    // fewer than 6 focusables on the page).
    let reachedEmail = false;
    for (let i = 0; i < 10; i++) {
      await page.keyboard.press("Tab");
      if (await email.evaluate((el) => el === document.activeElement)) {
        reachedEmail = true;
        break;
      }
    }
    expect(reachedEmail, "email input must be reachable via Tab").toBe(true);

    // From the email field, one more Tab lands on Send magic link.
    await page.keyboard.press("Tab");
    const sendFocused = await send.evaluate(
      (el) => el === document.activeElement,
    );
    expect(
      sendFocused,
      "Send magic link button must follow the email field in tab order",
    ).toBe(true);
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
