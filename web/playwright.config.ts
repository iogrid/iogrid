import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright configuration for the iogrid web plane.
 *
 * Three test trees are wired:
 *   - tests/                    — placeholder smoke specs (string-only,
 *                                 always green, run in every project).
 *   - tests/e2e/                — real end-to-end flows that boot
 *                                 `pnpm dev` against the built Next.js
 *                                 server, exercise the routing/nav, and
 *                                 walk pages that only require server
 *                                 components (no live backend needed).
 *   - tests/a11y/               — axe-core WCAG 2.2 AA scans. Severity
 *                                 >= "serious" fails the run. Keyboard
 *                                 navigation order is asserted alongside.
 *
 * The dev server is booted via Playwright's `webServer`. We use
 * `pnpm dev` instead of `pnpm build && pnpm start` because:
 *   - the test target is reachability + a11y + nav contract, not
 *     production bundle sizes (covered by `pnpm build` in web-ci).
 *   - dev mode warms in ~6s on github-hosted runners; production
 *     mode takes 45-90s (next build) for negligible test value.
 *
 * Env wiring (also documented in .env.example and the web-e2e workflow):
 *   AUTH_SECRET                  any 32B string; required by NextAuth at boot
 *   NEXTAUTH_URL                 http://localhost:3000
 *   EMAIL_SERVER                 a stub SMTP URL (smtp://localhost:1025) so
 *                                NextAuth's nodemailer provider initialises
 *                                without contacting real Stalwart.
 *   EMAIL_FROM                   any address; never actually sent.
 */
export default defineConfig({
  testDir: "./tests",
  testIgnore: ["**/*.placeholder.ts"],
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI
    ? [
        ["html", { open: "never", outputFolder: "playwright-report" }],
        ["github"],
        ["list"],
      ]
    : "html",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  webServer: process.env.PLAYWRIGHT_SKIP_WEBSERVER
    ? undefined
    : {
        command: "pnpm dev",
        url: "http://localhost:3000",
        reuseExistingServer: !process.env.CI,
        timeout: 120_000,
        stdout: "pipe",
        stderr: "pipe",
        env: {
          NEXT_TELEMETRY_DISABLED: "1",
          AUTH_SECRET:
            process.env.AUTH_SECRET ??
            "ci-placeholder-auth-secret-do-not-use-in-prod",
          NEXTAUTH_URL: "http://localhost:3000",
          EMAIL_SERVER:
            process.env.EMAIL_SERVER ?? "smtp://localhost:1025",
          EMAIL_FROM:
            process.env.EMAIL_FROM ?? "noreply@iogrid.test",
        },
      },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"] },
    },
    {
      name: "webkit",
      use: { ...devices["Desktop Safari"] },
    },
  ],
});
