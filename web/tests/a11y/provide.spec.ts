import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

/**
 * Accessibility — WCAG 2.2 AA scan of the publicly-reachable surfaces.
 *
 * Same pass/fail policy as account.spec.ts:
 *   - impact >= "serious" → fail
 *   - impact "minor"/"moderate" → log + follow-up issue
 *
 * Routes covered:
 *   - / (marketing homepage — already in account.spec but kept here
 *     for fan-out parallelism)
 *   - /install (the grandma button — copy buttons, details/summary
 *     toggles, signed-package download links)
 *   - /vpn (consumer SOCKS5 landing — many CTAs)
 *   - /onboard (the pairing-code paste-in landing)
 *
 * Protected portal routes (/provide/*, /customer/*, /admin/*) require
 * an authenticated session — the middleware short-circuits them to
 * /account, so scanning them would just rescan the sign-in panel.
 * They get a dedicated a11y pass in the follow-up PR once the
 * mock-session-cookie helper is added.
 */

const SEVERE = new Set(["serious", "critical"]);

const DEV_OVERLAY_SELECTORS = [
  "nextjs-portal",
  "[data-nextjs-toast]",
  "[data-nextjs-dialog]",
  "[data-nextjs-dialog-overlay]",
  "[data-nextjs-call-stack-frame]",
  "[data-nextjs-scroll-focus-boundary]",
  "[data-has-source]",
];

async function scan(page: Page, label: string) {
  let builder = new AxeBuilder({ page }).withTags([
    "wcag2a",
    "wcag2aa",
    "wcag21a",
    "wcag21aa",
    "wcag22aa",
    "best-practice",
  ]);
  for (const sel of DEV_OVERLAY_SELECTORS) {
    builder = builder.exclude(sel);
  }
  const results = await builder.analyze();

  const severe = results.violations.filter((v) =>
    SEVERE.has(v.impact ?? "minor"),
  );
  if (severe.length > 0) {
    console.error(
      `[a11y:${label}] ${severe.length} serious/critical violations:`,
      severe.map((v) => ({
        id: v.id,
        impact: v.impact,
        help: v.help,
        helpUrl: v.helpUrl,
        nodes: v.nodes.map((n) => n.target),
      })),
    );
  }
  expect(severe, `Serious WCAG violations on ${label}`).toEqual([]);
}

const ROUTES: Array<{ path: string; label: string }> = [
  { path: "/", label: "home" },
  { path: "/install", label: "install" },
  { path: "/vpn", label: "vpn" },
];

for (const route of ROUTES) {
  test(`WCAG 2.2 AA — ${route.path}`, async ({ page }) => {
    await page.goto(route.path, { waitUntil: "domcontentloaded" });
    await scan(page, route.label);
  });
}

test("keyboard nav — every public shell route exposes a <main> landmark", async ({
  page,
}) => {
  for (const route of ["/", "/install", "/account", "/vpn"]) {
    await page.goto(route, { waitUntil: "domcontentloaded" });
    await page
      .locator("main")
      .first()
      .waitFor({ state: "attached", timeout: 10_000 });
    const mainCount = await page.locator("main").count();
    expect(
      mainCount,
      `${route} must expose at least one <main> landmark`,
    ).toBeGreaterThan(0);
  }
});
