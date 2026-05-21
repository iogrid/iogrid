import { test, expect } from "@playwright/test";

/**
 * E2E — admin vs user-facing separation invariant (EPIC #422 Phase 4.1).
 *
 * Founder directive (2026-05-21):
 *   "admis app and user apps cannot be mixed to each other or instnace
 *    what is the point of showing the provide option to admin, he needs
 *    to access from teh eother indepent apps"
 *
 * This spec is the canonical anti-regression gate on the user-facing
 * web/ codebase. It asserts TWO properties (see docs/ARCHITECTURE.md
 * §4.6 for the full invariant + enforcement mechanisms):
 *
 *   1. No surface in web/ renders a link to `/admin/*`. The admin
 *      console lives on admin.iogrid.org, a wholly separate Next.js
 *      app. Any link from web/ into /admin is by definition broken
 *      (web/ has no /admin routes) AND a UX-leakage of admin context
 *      into a user-facing screen.
 *
 *   2. Every `/admin*` path in web/ returns a 404 (not a 200 from a
 *      stray route, not a 5xx from a half-deleted page, not a 302 to
 *      anywhere on web/). Admin routes simply do not exist in this
 *      codebase.
 *
 * If a future PR re-introduces /admin routes into web/ or adds a
 * convenience "Admin" link to PortalShell, one of these assertions
 * fails and the PR is blocked at CI before merge.
 */

// User-facing pages we walk anonymously to assert no /admin links are
// rendered. The landing + sign-in surface are public; deeper paths
// (/provide, /customer, /vpn) redirect anonymous users to /account
// via the middleware (see middleware-protected.spec.ts).
const ANONYMOUS_USER_FACING_PAGES = ["/", "/account"];

// Admin-path probes — every one must 404 inside web/.
const ADMIN_PATHS_THAT_MUST_404 = [
  "/admin",
  "/admin/",
  "/admin/abuse",
  "/admin/health",
  "/admin/providers",
  "/admin/billing",
];

test.describe("web/ separation invariant (EPIC #422 Phase 4.1)", () => {
  for (const target of ANONYMOUS_USER_FACING_PAGES) {
    test(`user-facing ${target} renders zero links to /admin`, async ({
      page,
    }) => {
      const response = await page.goto(target, {
        waitUntil: "domcontentloaded",
      });
      expect(
        response?.status(),
        `GET ${target} must not 5xx`,
      ).toBeLessThan(500);

      // Sweep every <a href> on the rendered surface. We allow any
      // href that does NOT begin with "/admin" or point to
      // admin.iogrid.org. The shell is the place admin links would
      // sneak in (PortalShell, marketing nav, footer), so we audit
      // ALL anchors — not just the nav.
      const offenders = await page.$$eval("a[href]", (anchors) =>
        anchors
          .map((a) => a.getAttribute("href") ?? "")
          .filter((href) => {
            // Same-origin /admin or /admin/...
            if (href === "/admin") return true;
            if (href.startsWith("/admin/")) return true;
            if (href.startsWith("/admin?")) return true;
            // Cross-origin admin.iogrid.org links — also disallowed
            // in user-facing surfaces. The user-facing app must NOT
            // advertise the admin host; admins arrive there via
            // bookmark or external nav, not via a user-facing link.
            if (/https?:\/\/admin\.iogrid\.org/i.test(href)) return true;
            return false;
          }),
      );
      expect(
        offenders,
        `${target} rendered links pointing at admin surfaces: ` +
          `${JSON.stringify(offenders)}. The user-facing web/ app must ` +
          `NEVER advertise the admin console — admins reach it by ` +
          `bookmark on admin.iogrid.org. See docs/ARCHITECTURE.md §4.6.`,
      ).toEqual([]);
    });
  }

  for (const adminPath of ADMIN_PATHS_THAT_MUST_404) {
    test(`admin path ${adminPath} returns 404 inside web/`, async ({
      page,
    }) => {
      const response = await page.goto(adminPath, {
        waitUntil: "domcontentloaded",
      });
      const status = response?.status();
      // We assert 404 specifically — not "any non-2xx". A 302 (e.g.
      // middleware redirect to /account) would mean web/ still treats
      // /admin as a protected route inside its own codebase, which is
      // exactly the failure mode EPIC #422 Phase 1 fixed. The admin
      // routes simply do not exist in web/ anymore; Next.js MUST
      // return 404.
      expect(
        status,
        `GET ${adminPath} on web/ returned ${status}. After EPIC #422 ` +
          `Phase 1 the admin routes were moved out of web/ entirely; ` +
          `requests for /admin/* MUST 404 (not redirect, not render). ` +
          `If this is failing, web/ has re-grown admin routes. See ` +
          `docs/ARCHITECTURE.md §4.6.`,
      ).toBe(404);
    });
  }
});
