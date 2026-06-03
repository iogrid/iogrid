import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright configuration for the iogrid web plane.
 *
 * Three test trees are wired:
 *   - tests/                    — placeholder smoke specs (string-only,
 *                                 always green, kept for the legacy
 *                                 web-ci pipeline).
 *   - tests/e2e/                — real end-to-end flows that boot the
 *                                 built Next.js server, exercise the
 *                                 routing/nav, and walk pages that only
 *                                 require server components (no live
 *                                 backend needed).
 *   - tests/a11y/               — axe-core WCAG 2.2 AA scans. Severity
 *                                 >= "serious" fails the run. Keyboard
 *                                 navigation order is asserted alongside.
 *
 * Production-mode server (`pnpm build && pnpm start`) — NOT `pnpm dev`.
 * The dev mode injects the Next.js dev-overlay portal (a floating
 * "open in editor" panel with `tabindex="10"` + `nextjs-portal` root)
 * which trips multiple axe rules (`tabindex`, `aria-allowed-attr`)
 * and shifts the keyboard-nav order. It also pulls nodemailer into
 * the edge runtime on first navigation, throwing a hard error on
 * /provide. Production builds strip the overlay entirely.
 *
 * Env wiring (also documented in the web-e2e / web-a11y workflows):
 *   AUTH_SECRET                  any 32B string; required by NextAuth at boot
 *   NEXTAUTH_URL                 http://localhost:3000
 *   EMAIL_SERVER_HOST/PORT/USER/PASSWORD
 *                                stub SMTP creds so NextAuth's nodemailer
 *                                provider initialises without contacting
 *                                real Stalwart.
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
        // Production build + start. Build runs once; reuseExistingServer
        // makes local re-runs cheap.
        command:
          process.env.PLAYWRIGHT_WEB_COMMAND ??
          "pnpm build && pnpm start -p 3000",
        url: "http://localhost:3000",
        reuseExistingServer: !process.env.CI,
        timeout: 240_000,
        stdout: "pipe",
        stderr: "pipe",
        env: {
          NEXT_TELEMETRY_DISABLED: "1",
          AUTH_SECRET:
            process.env.AUTH_SECRET ??
            "ci-placeholder-auth-secret-do-not-use-in-prod",
          NEXTAUTH_URL: "http://localhost:3000",
          // auth.js (NextAuth v5) rejects requests whose Host isn't trusted
          // unless this is set — without it the E2E run floods with
          // `UntrustedHost: Host must be trusted` and the auth/session-backed
          // specs fail (#671). localhost dev/CI is a trusted host.
          AUTH_TRUST_HOST: "true",
          // NextAuth nodemailer provider expects the *_HOST/_PORT family.
          EMAIL_SERVER_HOST: process.env.EMAIL_SERVER_HOST ?? "localhost",
          EMAIL_SERVER_PORT: process.env.EMAIL_SERVER_PORT ?? "1025",
          EMAIL_SERVER_USER: process.env.EMAIL_SERVER_USER ?? "test",
          EMAIL_SERVER_PASSWORD:
            process.env.EMAIL_SERVER_PASSWORD ?? "test",
          EMAIL_FROM: process.env.EMAIL_FROM ?? "noreply@iogrid.test",
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
