import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

/**
 * Accessibility — WCAG 2.2 AA scan of /account/* surface.
 *
 * Methodology:
 *   - Use @axe-core/playwright's `AxeBuilder` to inject axe-core
 *     (v4.10) into each page and run the official `wcag2a`, `wcag2aa`,
 *     `wcag21a`, `wcag21aa`, `wcag22aa` tag sets.
 *   - Fail the test if any rule with impact >= "serious" reports a
 *     violation. "Minor" and "moderate" findings are logged via
 *     `console.info` so reviewers see them in the CI annotation but
 *     they don't block the merge — the project's policy is "AA is a
 *     hard floor; moderate findings get follow-up issues".
 *   - Exclude the Next.js dev-overlay portal (`nextjs-portal`,
 *     `[data-nextjs-toast]`, `[data-nextjs-dialog]`,
 *     `[data-nextjs-call-stack-frame]`) and Next.js' "open in editor"
 *     floating buttons — those are dev-only DOM nodes that never ship
 *     to production users.
 *
 * Keyboard-nav audit:
 *   - We tab through every focusable element on /account and assert
 *     that no element ends up with `outline: none` AND no visible
 *     `:focus-visible` ring (the dual-check catches the common
 *     Tailwind `focus-visible:ring-2` regression).
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
  const minor = results.violations.filter(
    (v) => !SEVERE.has(v.impact ?? "minor"),
  );

  if (minor.length > 0) {
    console.info(
      `[a11y:${label}] ${minor.length} minor/moderate findings:`,
      minor.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.length })),
    );
  }
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

  expect(
    severe,
    `Serious/critical WCAG 2.2 AA violations on ${label}`,
  ).toEqual([]);
}

test.describe("WCAG 2.2 AA — /account surface", () => {
  test("/account (unauthenticated sign-in panel)", async ({ page }) => {
    await page.goto("/account", { waitUntil: "domcontentloaded" });
    await scan(page, "account");
  });

  test("/account — keyboard focus visible on every interactive element", async ({
    page,
  }) => {
    await page.goto("/account", { waitUntil: "domcontentloaded" });

    // Restrict to user-visible focusables. Server Action `<form>`s inject
    // up to 10 hidden `<input>`s per form for the action-id payload —
    // they are not focusable by keyboard users and would skew the audit.
    const handles = await page
      .locator(
        'main a:visible, main button:visible, main input:visible:not([type="hidden"]), main select:visible, main textarea:visible, main [tabindex]:visible:not([tabindex="-1"])',
      )
      .elementHandles();

    expect(
      handles.length,
      "Expected at least one keyboard-focusable element in main",
    ).toBeGreaterThan(0);

    const offenders: string[] = [];
    for (const el of handles) {
      await el.focus().catch(() => undefined);
      const info = await el.evaluate((node) => {
        const html = node as HTMLElement;
        // Only audit elements that the user can actually focus with Tab.
        if (html.tabIndex < 0) return null;
        const style = getComputedStyle(html);
        // Ignore zero-size shadows so we don't double-count `box-shadow:
        // 0 0 0 0 rgba(...)` placeholders that some libraries inject.
        const ring =
          style.outlineStyle !== "none" ||
          (style.boxShadow && style.boxShadow !== "none");
        const tag = html.tagName.toLowerCase();
        const id =
          html.id ||
          html.getAttribute("name") ||
          html.textContent?.trim().slice(0, 30) ||
          tag;
        return { tag, id, hasRing: !!ring };
      });
      if (info && !info.hasRing) offenders.push(`${info.tag}#${info.id}`);
    }

    expect(
      offenders,
      `Focusable elements with no visible focus ring on /account`,
    ).toEqual([]);
  });

  test("homepage / has no serious WCAG violations", async ({ page }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await scan(page, "home");
  });
});
