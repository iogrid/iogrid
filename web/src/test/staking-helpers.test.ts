import { describe, expect, it } from "vitest";
import {
  STAKING_TIERS,
  formatRemainingLock,
  tierFor,
} from "@/lib/solana/staking";

describe("staking tier table", () => {
  it("matches docs/TOKENOMICS.md §Layer-3 tiers", () => {
    expect(STAKING_TIERS.map((t) => t.lockPeriodDays)).toEqual([30, 90, 180, 365]);
    expect(STAKING_TIERS.map((t) => t.multiplier)).toEqual([1.0, 1.25, 1.5, 2.0]);
  });

  it("tierFor returns the matching tier", () => {
    expect(tierFor(180).name).toBe("Conviction");
    expect(tierFor(365).multiplier).toBe(2.0);
  });

  it("falls back to Standard for an unknown period", () => {
    // @ts-expect-error — exercising the fallback
    expect(tierFor(7).name).toBe("Standard");
  });
});

describe("formatRemainingLock", () => {
  const NOW = Date.parse("2026-05-19T00:00:00Z");

  it("returns 'Unlocked' for past unlocks", () => {
    expect(formatRemainingLock("2026-05-01T00:00:00Z", NOW)).toBe("Unlocked");
  });

  it("formats >1 day as days + hours", () => {
    const future = new Date(NOW + 36 * 3600 * 1000).toISOString();
    expect(formatRemainingLock(future, NOW)).toBe("1d 12h");
  });

  it("formats <1 day as hours + minutes", () => {
    const future = new Date(NOW + 2 * 3600 * 1000 + 30 * 60 * 1000).toISOString();
    expect(formatRemainingLock(future, NOW)).toBe("2h 30m");
  });
});
