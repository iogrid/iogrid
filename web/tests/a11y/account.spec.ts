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

    const handles = await page
      .locator(
        'main a, main button, main input, main select, main textarea, main [tabindex]:not([tabindex="-1"])',
      )
      .elementHandles();

    expect(handles.length).toBeGreaterThan(0);

    const offenders: string[] = [];
    for (const el of handles) {
      await el.focus().catch(() => undefined);
      const info = await el.evaluate((node) => {
        const style = getComputedStyle(node as HTMLElement);
        const tag = (node as HTMLElement).tagName.toLowerCase();
        const id =
          (node as HTMLElement).id ||
          (node as HTMLElement).textContent?.slice(0, 30) ||
          tag;
        const hasRing =
          style.outlineStyle !== "none" ||
          (style.boxShadow !== "none" && style.boxShadow !== "");
        return { tag, id, hasRing };
      });
      if (!info.hasRing) offenders.push(`${info.tag}#${info.id}`);
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
