import { describe, it, expect, beforeEach } from "vitest";
import {
  loadPendingOffRamps,
  rememberOffRamp,
  forgetOffRamp,
  type PendingOffRamp,
} from "@/app/provider/earnings/withdraw";

describe("withdraw localStorage helpers", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("returns empty list when storage is empty", () => {
    expect(loadPendingOffRamps()).toEqual([]);
  });

  it("remembers and reads back a pending request", () => {
    const row: PendingOffRamp = {
      requestId: "req-1",
      providerName: "moonpay",
      startedAt: "2026-05-19T10:00:00Z",
    };
    rememberOffRamp(row);
    expect(loadPendingOffRamps()).toEqual([row]);
  });

  it("deduplicates by request id when remembering twice", () => {
    rememberOffRamp({
      requestId: "req-1",
      providerName: "moonpay",
      startedAt: "2026-05-19T10:00:00Z",
    });
    rememberOffRamp({
      requestId: "req-1",
      providerName: "moonpay",
      startedAt: "2026-05-19T10:05:00Z",
    });
    const got = loadPendingOffRamps();
    expect(got).toHaveLength(1);
    expect(got[0].startedAt).toBe("2026-05-19T10:05:00Z");
  });

  it("forgets a request id", () => {
    rememberOffRamp({
      requestId: "req-1",
      providerName: "moonpay",
      startedAt: "2026-05-19T10:00:00Z",
    });
    rememberOffRamp({
      requestId: "req-2",
      providerName: "sociable-cash",
      startedAt: "2026-05-19T10:05:00Z",
    });
    forgetOffRamp("req-1");
    const got = loadPendingOffRamps();
    expect(got).toHaveLength(1);
    expect(got[0].requestId).toBe("req-2");
  });

  it("tolerates corrupt JSON in storage", () => {
    window.localStorage.setItem("iogrid_offramp_pending", "{not json");
    expect(loadPendingOffRamps()).toEqual([]);
  });

  it("caps the stored list to prevent unbounded growth", () => {
    for (let i = 0; i < 20; i++) {
      rememberOffRamp({
        requestId: `req-${i}`,
        providerName: "moonpay",
        startedAt: new Date(2026, 0, 1, i).toISOString(),
      });
    }
    const got = loadPendingOffRamps();
    // Implementation truncates to the most recent 10 entries.
    expect(got.length).toBeLessThanOrEqual(10);
    expect(got[got.length - 1].requestId).toBe("req-19");
  });
});
