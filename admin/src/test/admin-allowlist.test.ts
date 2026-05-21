import { describe, expect, it } from "vitest";
import { isAdminEmail, parseAdminEmails } from "@/lib/admin-allowlist";

/**
 * Unit tests for the IOGRID_ADMIN_EMAILS gating helpers used by the
 * edge middleware. The middleware itself is hard to test outside the
 * runtime — these pure helpers carry the policy and are import-safe.
 */
describe("parseAdminEmails", () => {
  it("returns an empty set for undefined / empty", () => {
    expect(parseAdminEmails(undefined).size).toBe(0);
    expect(parseAdminEmails("").size).toBe(0);
    expect(parseAdminEmails("   ").size).toBe(0);
  });

  it("lowercases + trims comma-separated entries", () => {
    const s = parseAdminEmails("  Emrah.Baysal@OpenOva.io ,Hatice@openova.io");
    expect(s.has("emrah.baysal@openova.io")).toBe(true);
    expect(s.has("hatice@openova.io")).toBe(true);
    expect(s.size).toBe(2);
  });

  it("drops blank entries from trailing commas", () => {
    const s = parseAdminEmails("a@x.io,,b@x.io,");
    expect(s.size).toBe(2);
    expect([...s].sort()).toEqual(["a@x.io", "b@x.io"]);
  });
});

describe("isAdminEmail", () => {
  const allow = "Emrah.Baysal@openova.io,hatice@openova.io";

  it("is true for an allowlisted email regardless of case", () => {
    expect(isAdminEmail("emrah.baysal@openova.io", allow)).toBe(true);
    expect(isAdminEmail("Emrah.Baysal@OpenOva.io", allow)).toBe(true);
  });

  it("is false for a non-allowlisted email", () => {
    expect(isAdminEmail("attacker@example.com", allow)).toBe(false);
  });

  it("is false when no email is provided", () => {
    expect(isAdminEmail(undefined, allow)).toBe(false);
    expect(isAdminEmail(null, allow)).toBe(false);
    expect(isAdminEmail("", allow)).toBe(false);
  });

  it("is false when the allowlist env var is missing", () => {
    expect(isAdminEmail("emrah.baysal@openova.io", undefined)).toBe(false);
    expect(isAdminEmail("emrah.baysal@openova.io", "")).toBe(false);
  });
});
