import { test, expect } from "@playwright/test";

/**
 * E2E — middleware gate on /provide, /customer, /admin.
 *
 * Regression test for #204: previously, `src/middleware.ts` imported
 * `@/lib/auth`, which transitively pulled `nodemailer`. The edge runtime
 * does not support `stream` / `setImmediate`, so module-eval threw
 *   `TypeError: Cannot redefine property: __import_unsupported`
 * The middleware silently failed and the protected route rendered
 * instead of redirecting an unauthenticated visitor to /account.
 *
 * After the split-auth fix (`@/auth.config` is edge-safe, `@/lib/auth`
 * is node-only), the middleware boots cleanly in the edge runtime and
 * the unauthenticated visitor lands on /account with a `callbackUrl`
 * preserving their original destination.
 *
 * We assert the strong contract: the response must NOT be a 5xx, and
 * the URL must end on /account?callbackUrl=<original>. We do NOT assert
 * a specific 302 vs 307 — Next.js may use either for middleware
 * redirects depending on the request method.
 */
test.describe("middleware — protected route gate (no edge crash)", () => {
  // /admin moved to its own Next.js app (admin.iogrid.org) in #361, so
  // this spec only covers the two surfaces still inside `web/`.
  for (const target of ["/provide", "/customer"]) {
    test(`unauthenticated GET ${target} redirects to /account`, async ({
      page,
    }) => {
      const response = await page.goto(target, {
        waitUntil: "domcontentloaded",
      });

      // Hard contract: no 5xx. If the edge runtime crashes we get a 500
      // here and the regression is back.
      expect(
        response?.status(),
        `GET ${target} must not 5xx (edge runtime crash)`,
      ).toBeLessThan(500);

      // The middleware sends us to /account with a callbackUrl that
      // preserves the original destination so the user lands back where
      // they intended after signing in.
      await expect(page).toHaveURL(/\/account\?callbackUrl=/);
      const url = new URL(page.url());
      expect(url.searchParams.get("callbackUrl")).toBe(target);

      // The sign-in surface must render (not a blank page from a half-
      // crashed middleware).
      await expect(
        page.getByRole("heading", { name: /sign in to iogrid/i }),
      ).toBeVisible();
    });
  }
});
