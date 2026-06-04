import { render, screen, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { StatusPageClient } from "@/app/status/status-page-client";

/**
 * StatusPageClient (#674/#689) — the live /status island.
 *
 * Pins the three behaviors reviewers called out:
 *  - honest fallback (NEVER fake-green) when the feed is unreachable;
 *  - posture rendering incl. the plain-string `overall` shape the real
 *    feed emits (the "Unknown" banner bug, 3f6e9f2d);
 *  - per-service uptime strips fetched once per service (#689), with
 *    no-data days rendered neutral.
 */

const POSTURE = {
  schema_version: 1,
  generated_at: "2026-06-04T00:00:00Z",
  overall: "up", // plain string — the real wire shape
  services: [
    { name: "identity-svc", status: "up", slo_percent: 95 },
    { name: "web", status: "degraded", slo_percent: 99 },
  ],
  incidents_active: [],
  incidents_recent: [],
};

const UPTIME = {
  days: 3,
  samples: [
    { service: "identity-svc", day: "2026-06-01", state: "", sli_pct: 0 },
    { service: "identity-svc", day: "2026-06-02", state: "up", sli_pct: 99.99 },
    { service: "identity-svc", day: "2026-06-03", state: "down", sli_pct: 42 },
  ],
};

describe("StatusPageClient", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("renders the honest unavailable card when the feed fails — never fake-green", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 502 } as Response),
    );
    render(<StatusPageClient />);
    await waitFor(() =>
      expect(screen.getByTestId("status-feed-unavailable")).toBeInTheDocument(),
    );
    expect(screen.queryByText(/All systems operational/i)).toBeNull();
  });

  it("renders the string-shaped overall as 'All systems operational' + service rows", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve(String(url).includes("kind=uptime") ? UPTIME : POSTURE),
        } as Response),
      ),
    );
    render(<StatusPageClient />);
    await waitFor(() =>
      expect(screen.getByText("All systems operational")).toBeInTheDocument(),
    );
    expect(screen.getByText("identity-svc")).toBeInTheDocument();
    expect(screen.getByText(/SLO budget 95\.0%/)).toBeInTheDocument();
  });

  it("fetches uptime once per service and renders the strips", async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) =>
      Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve(String(url).includes("kind=uptime") ? UPTIME : POSTURE),
      } as Response),
    );
    vi.stubGlobal("fetch", fetchMock);
    render(<StatusPageClient />);
    await waitFor(() =>
      expect(screen.getAllByTestId("uptime-strip").length).toBeGreaterThan(0),
    );
    const uptimeCalls = fetchMock.mock.calls
      .map((c) => String(c[0]))
      .filter((u) => u.includes("kind=uptime"));
    // one fetch per service, each with the validated service param
    expect(uptimeCalls).toHaveLength(POSTURE.services.length);
    expect(uptimeCalls.some((u) => u.includes("service=identity-svc"))).toBe(true);
  });
});
