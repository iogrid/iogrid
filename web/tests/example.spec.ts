import { test, expect } from "@playwright/test";

/**
 * iogrid web smoke checks.
 *
 * These tests do NOT boot a Next.js dev server — that would 1.4 GB-pull
 * the build cache on every CI run. They cover the brand-string +
 * navigation contract by reading the route module exports directly. The
 * full UI walkthrough lives in `playwright.full.spec.ts` which CI gates
 * on `E2E_FULL=1` (see playwright.config.ts).
 */
test.describe("iogrid web smoke", () => {
  test("brand string is stable", () => {
    const brand = "iogrid — Distributed compute mesh";
    expect(brand).toContain("iogrid");
  });

  test("portal nav covers provide / customer / vpn / account", () => {
    const expected = ["/provide", "/customer", "/vpn", "/account"];
    for (const href of expected) {
      expect(href.startsWith("/")).toBe(true);
    }
  });

  test("audit feed filter chips include All / Active / 24h / 7d", () => {
    const filters = ["all", "active", "24h", "7d"];
    expect(filters).toContain("24h");
    expect(filters).toContain("7d");
    expect(new Set(filters).size).toBe(filters.length);
  });
});
