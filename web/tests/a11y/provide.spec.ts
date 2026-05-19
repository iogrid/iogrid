import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

/**
 * Accessibility — WCAG 2.2 AA scan of /provide/* and /customer/*.
 *
 * Same pass/fail policy as account.spec.ts:
 *   - impact >= "serious" → fail
 *   - impact "minor"/"moderate" → log + follow-up issue
 *
 * Routes covered:
 *   - /provide (operator overview)
 *   - /provide/audit (audit feed — must be reachable from screen
 *     readers; the chip filter row is high-risk for missing
 *     aria-pressed on toggles).
 *   - /customer (workspace overview)
 *   - /customer/api-keys (the workspace empty-state callout, which
 *     is high-risk for low contrast on amber-on-amber colours).
 *   - /install (the grandma button — copy buttons + tabs).
 *   - /vpn (consumer SOCKS5 landing — many CTAs).
 */

const SEVERE = new Set(["serious", "critical"]);

async function scan(page: import("@playwright/test").Page, label: string) {
  const results = await new AxeBuilder({ page })
    .withTags([
      "wcag2a",
      "wcag2aa",
      "wcag21a",
      "wcag21aa",
      "wcag22aa",
      "best-practice",
    ])
    .analyze();

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
  { path: "/provide", label: "provide" },
  { path: "/provide/audit", label: "provide-audit" },
  { path: "/provide/earnings", label: "provide-earnings" },
  { path: "/customer", label: "customer" },
  { path: "/customer/api-keys", label: "customer-api-keys" },
  { path: "/install", label: "install" },
  { path: "/vpn", label: "vpn" },
];

for (const route of ROUTES) {
  test(`WCAG 2.2 AA — ${route.path}`, async ({ page }) => {
    await page.goto(route.path, { waitUntil: "domcontentloaded" });
    await scan(page, route.label);
  });
}

test("keyboard nav — top-of-page Skip link or main landmark on every shell route", async ({
  page,
}) => {
  for (const route of [
    "/provide",
    "/customer",
    "/customer/api-keys",
    "/account",
  ]) {
    await page.goto(route, { waitUntil: "domcontentloaded" });
    // Either an explicit "skip to main" link OR a <main> landmark.
    const skip = page.getByRole("link", { name: /skip to (main|content)/i });
    const main = page.locator("main");
    const hasLandmark = (await main.count()) > 0;
    const hasSkip = (await skip.count()) > 0;
    expect(
      hasLandmark || hasSkip,
      `${route} must expose a <main> landmark or a Skip link`,
    ).toBe(true);
  }
});
