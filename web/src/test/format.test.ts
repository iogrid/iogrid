import { describe, expect, it } from "vitest";
import {
  categoryLabel,
  eventKindGlyph,
  eventKindLabel,
  formatBytes,
  formatMoney,
  formatRelativeTime,
} from "@/lib/format";

describe("formatBytes", () => {
  it("returns 0 B for falsy / non-positive input", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes("")).toBe("0 B");
    expect(formatBytes(-12)).toBe("0 B");
  });

  it("renders bytes without decimals", () => {
    expect(formatBytes(512)).toBe("512 B");
  });

  it("scales to KB / MB / GB with one decimal", () => {
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1024 * 1024)).toBe("1.0 MB");
    expect(formatBytes(1024 ** 3)).toBe("1.0 GB");
  });

  it("accepts numeric strings (proto3 uint64 wire format)", () => {
    expect(formatBytes("2048")).toBe("2.0 KB");
  });
});

describe("formatRelativeTime", () => {
  const now = Date.UTC(2026, 0, 1, 12, 0, 0);

  it("returns 'just now' inside 5s", () => {
    const iso = new Date(now - 2_000).toISOString();
    expect(formatRelativeTime(iso, now)).toBe("just now");
  });

  it("returns seconds when below 1m", () => {
    const iso = new Date(now - 12_000).toISOString();
    expect(formatRelativeTime(iso, now)).toBe("12s ago");
  });

  it("returns minutes / hours / days as appropriate", () => {
    expect(formatRelativeTime(new Date(now - 120_000).toISOString(), now)).toBe(
      "2m ago",
    );
    expect(
      formatRelativeTime(new Date(now - 7_200_000).toISOString(), now),
    ).toBe("2h ago");
    expect(
      formatRelativeTime(new Date(now - 2 * 86_400_000).toISOString(), now),
    ).toBe("2d ago");
  });

  it("returns '—' on garbage input", () => {
    expect(formatRelativeTime(undefined, now)).toBe("—");
    expect(formatRelativeTime("not-a-date", now)).toBe("—");
  });
});

describe("formatMoney", () => {
  it("formats USD with the Intl API", () => {
    expect(formatMoney("12.34", "USD")).toBe("$12.34");
  });

  it("returns em-dash for empty ISO-currency inputs", () => {
    expect(formatMoney(undefined)).toBe("—");
    expect(formatMoney("")).toBe("—");
    expect(formatMoney(undefined, "EUR")).toBe("—");
  });

  // The $GRID branch is the Phase-0 native ledger currency. The
  // headline card on /provide/earnings reads `currencyCode ?? "GRID"`,
  // so this function must produce the "0 $GRID" empty-state copy when
  // proto3 wire-omits the zero amount — NOT em-dash (#312).
  describe("$GRID (Phase-0 native ledger)", () => {
    it("renders '0 $GRID' for an undefined / empty amount", () => {
      expect(formatMoney(undefined, "GRID")).toBe("0 $GRID");
      expect(formatMoney(null as unknown as undefined, "GRID")).toBe("0 $GRID");
      expect(formatMoney("", "GRID")).toBe("0 $GRID");
    });

    it("renders '0 $GRID' for a literal zero (string or number)", () => {
      expect(formatMoney("0", "GRID")).toBe("0 $GRID");
      expect(formatMoney(0, "GRID")).toBe("0 $GRID");
    });

    it("renders whole $GRID amounts with locale grouping, no decimals", () => {
      expect(formatMoney("1", "GRID")).toBe("1 $GRID");
      expect(formatMoney("1234", "GRID")).toBe("1,234 $GRID");
      expect(formatMoney(1234567, "GRID")).toBe("1,234,567 $GRID");
    });

    it("renders fractional $GRID amounts, trimming trailing zeros", () => {
      expect(formatMoney("0.5", "GRID")).toBe("0.5 $GRID");
      expect(formatMoney("1.25", "GRID")).toBe("1.25 $GRID");
      expect(formatMoney("1.2500", "GRID")).toBe("1.25 $GRID");
      // Caps at 4dp.
      expect(formatMoney("0.12345", "GRID")).toBe("0.1235 $GRID");
    });

    it("never throws on the GRID code (it is NOT ISO-4217)", () => {
      // Sanity: confirm Intl.NumberFormat would throw — i.e. the
      // bespoke branch is load-bearing.
      expect(() =>
        new Intl.NumberFormat("en-US", { style: "currency", currency: "GRID" }),
      ).toThrow();
      // formatMoney must NOT throw.
      expect(formatMoney("1", "GRID")).toBe("1 $GRID");
    });
  });
});

describe("eventKindLabel/Glyph + categoryLabel", () => {
  it("returns canonical labels", () => {
    expect(eventKindLabel("EVENT_KIND_WORKLOAD_BLOCKED")).toBe(
      "Workload blocked",
    );
    expect(eventKindGlyph("EVENT_KIND_EARNINGS_CREDITED")).toBe("$");
  });

  /**
   * Regression for #314: gateway-bff emits proto enums as numeric tags
   * via encoding/json. The label/glyph helpers MUST canonicalise the
   * numeric form back to the proto's full SCREAMING_SNAKE_CASE name.
   */
  it("accepts the numeric proto tag (encoding/json wire form)", () => {
    expect(eventKindLabel(3)).toBe("Workload blocked");
    expect(eventKindLabel(6)).toBe("Earnings credited");
    expect(eventKindGlyph(6)).toBe("$");
    expect(eventKindGlyph(5)).toBe("!");
  });

  it("falls back to the default branch on unknown numeric tags", () => {
    expect(eventKindLabel(999)).toBe("Event");
    expect(eventKindGlyph(999)).toBe("·");
  });

  it("title-cases category slugs", () => {
    expect(categoryLabel("e_commerce")).toBe("E Commerce");
    expect(categoryLabel("seo")).toBe("Seo");
    expect(categoryLabel("")).toBe("General");
  });
});
