/**
 * @file EPIC #422 Phase 4.1 — admin/user-facing separation invariant.
 *
 * The founder's directive (2026-05-21):
 *   "admis app and user apps cannot be mixed to each other or instnace
 *    what is the point of showing the provide option to admin, he needs
 *    to access from teh eother indepent apps"
 *
 * This test is the canonical anti-regression gate. It enforces three
 * properties of the admin/ codebase that the separation invariant
 * relies on (docs/ARCHITECTURE.md §4.6):
 *
 *   1. `ADMIN_NAV` contains zero user-facing surface paths
 *      (/provide, /customer, /vpn — and known provider-side surfaces
 *      like /vcard if they were ever added).
 *   2. The admin/src/app/ route tree does not carry a `provide/`,
 *      `customer/`, or `vpn/` directory — those routes do not exist
 *      in this codebase and any request for them MUST 404.
 *   3. Rendering `AdminShell` with the canonical `ADMIN_NAV` produces
 *      zero `<a href="/provide…">` / `/customer…` / `/vpn…` links in
 *      the DOM.
 *
 * If a future PR adds a "this one admin button" exception to
 * `AdminShell` or a "convenience link to provider earnings" to
 * `ADMIN_NAV`, one of these assertions fails and the PR is blocked
 * at CI before merge. The invariant does not bend for convenience.
 */
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import fs from "node:fs";
import path from "node:path";

import { ADMIN_NAV } from "@/app/nav";
import { AdminShell } from "@/components/layout/admin-shell";

// AdminShell calls `auth()` server-side to read the signed-in email.
// In jsdom we have no NextAuth context — stub it so the async
// component resolves and renders.
vi.mock("@/lib/auth", () => ({
  auth: vi.fn(async () => ({
    user: { email: "ops@iogrid.org" },
  })),
}));

/**
 * Banned paths must match the user-facing route prefix EXACTLY at the
 * `/segment` boundary. We allow "/providers" (admin pool lookup) but
 * deny "/provide" and "/provide/anything". Same for "/customer" vs
 * imaginary "/customers" — we deny only the user-facing singular.
 */
const BANNED_PATHS = ["/provide", "/customer", "/vpn"];
const BANNED_ROUTE_DIRS = ["provide", "customer", "vpn"];

function pathHitsBanned(href: string): string | null {
  for (const banned of BANNED_PATHS) {
    if (href === banned || href.startsWith(banned + "/") || href.startsWith(banned + "?")) {
      return banned;
    }
  }
  return null;
}

describe("admin/ separation invariant (EPIC #422 Phase 4.1)", () => {
  it("ADMIN_NAV contains zero user-facing surface paths", () => {
    for (const item of ADMIN_NAV) {
      const hit = pathHitsBanned(item.href);
      expect(
        hit,
        hit
          ? `ADMIN_NAV entry { href: "${item.href}", label: "${item.label}" } ` +
              `points at user-facing surface "${hit}". The admin app must ` +
              `NEVER link to provider/customer/vpn surfaces — those live ` +
              `on the user-facing host (iogrid.org). See ` +
              `docs/ARCHITECTURE.md §4.6.`
          : "ok",
      ).toBeNull();
    }
  });

  it("admin/src/app/ tree has no provide/, customer/, vpn/ directories", () => {
    // Resolve admin/src/app/ relative to this test file
    // (admin/src/test/separation.test.tsx → admin/src/app).
    const appDir = path.resolve(__dirname, "..", "app");
    for (const banned of BANNED_ROUTE_DIRS) {
      const bannedDir = path.join(appDir, banned);
      expect(
        fs.existsSync(bannedDir),
        `admin/src/app/${banned}/ exists. User-facing route trees must ` +
          `NOT be present in the admin codebase — those routes live in ` +
          `web/. See docs/ARCHITECTURE.md §4.6.`,
      ).toBe(false);
    }
  });

  it("rendered AdminShell DOM contains no user-facing nav links", async () => {
    // AdminShell is an async server component; awaiting it resolves the
    // JSX before rendering through testing-library.
    const tree = await AdminShell({
      title: "Overview",
      nav: ADMIN_NAV,
      activeHref: "/",
      children: <div data-testid="admin-shell-children">child</div>,
    });
    render(tree);

    // Sanity: the shell actually rendered (otherwise we'd be asserting
    // absence on nothing — and the test would be a no-op).
    expect(screen.getByTestId("admin-shell-children")).toBeInTheDocument();

    // Sweep every anchor and assert no href starts with a banned prefix.
    // Anchors come from BOTH the top-bar (logo, sign-out) and the
    // section nav, so this catches any cross-bleed regardless of which
    // surface it appeared on.
    const anchors = document.querySelectorAll<HTMLAnchorElement>("a[href]");
    expect(anchors.length).toBeGreaterThan(0);
    for (const a of anchors) {
      const href = a.getAttribute("href") ?? "";
      const hit = pathHitsBanned(href);
      expect(
        hit,
        hit
          ? `AdminShell rendered <a href="${href}"> which points at ` +
              `user-facing surface "${hit}". Remove the link. See ` +
              `docs/ARCHITECTURE.md §4.6.`
          : "ok",
      ).toBeNull();
    }
  });
});
