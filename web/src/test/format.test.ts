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

  it("returns em-dash for empty inputs", () => {
    expect(formatMoney(undefined)).toBe("—");
    expect(formatMoney("")).toBe("—");
  });
});

describe("eventKindLabel/Glyph + categoryLabel", () => {
  it("returns canonical labels", () => {
    expect(eventKindLabel("EVENT_KIND_WORKLOAD_BLOCKED")).toBe(
      "Workload blocked",
    );
    expect(eventKindGlyph("EVENT_KIND_EARNINGS_CREDITED")).toBe("$");
  });

  it("title-cases category slugs", () => {
    expect(categoryLabel("e_commerce")).toBe("E Commerce");
    expect(categoryLabel("seo")).toBe("Seo");
    expect(categoryLabel("")).toBe("General");
  });
});
