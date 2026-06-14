/**
 * Vitest coverage for /account/sessions (issue #322).
 *
 * Pins:
 *   - 0-session response renders the empty-state copy.
 *   - 1-session response (the caller's own) renders the row + Current
 *     pill + disabled Revoke.
 *   - N-session response renders every row with humanised UA, IP,
 *     "started X ago" labels, and the current row sorted to the top.
 *   - Revoke button on a non-current row fires DELETE
 *     /api/v1/account/sessions/{id} and re-fetches.
 *   - API error sets the "Could not load" copy instead of the
 *     legacy "no other active sessions" empty state.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import { SessionsPanel } from "@/app/account/sessions/panel";

// Toast double — we don't care about render, just keep it from blowing up.
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Mock the api client so we can stub get/del per test.
const apiGet = vi.fn();
const apiDel = vi.fn();
vi.mock("@/lib/api", () => ({
  browserApi: () => ({ get: apiGet, del: apiDel }),
}));

const SID_CURRENT = "11111111-1111-1111-1111-111111111111";
const SID_OTHER1 = "22222222-2222-2222-2222-222222222222";
const SID_OTHER2 = "33333333-3333-3333-3333-333333333333";

// Anchor relative-time computations to the current wall clock so the
// "X ago" labels render against a stable delta without needing fake
// timers (which deadlock React's microtask scheduler under
// vitest/jsdom — manifests as 5000 ms waitFor timeouts).
const NOW = Date.now();
const isoMinusMin = (m: number) => new Date(NOW - m * 60_000).toISOString();
const isoPlusMin = (m: number) => new Date(NOW + m * 60_000).toISOString();

describe("SessionsPanel", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiDel.mockReset();
  });

  it("#808: renders 'this device' from the web-sessions feed even when the BFF feed has only non-current rows", async () => {
    // Reproduces the prod state surfaced by the 2026-06-14 UAT:
    //  - the identity-svc BFF feed returns ONLY stale rows with
    //    is_current=false (currentSID is unresolvable for a NextAuth
    //    request), blank UA, past expiry → "Unknown device · Expired";
    //  - the web-sessions feed (post-#808) synthesizes the current
    //    device. The panel merges both and MUST show the Current pill.
    apiGet.mockResolvedValueOnce({
      sessions: [
        {
          id: { value: SID_OTHER1 },
          user_agent: "",
          created_at: isoMinusMin(60 * 24 * 30),
          last_used_at: isoMinusMin(60 * 24 * 20),
          expires_at: isoMinusMin(60 * 24 * 5), // already expired
          is_current: false,
        },
      ],
    });
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        sessions: [
          {
            id: "abc1234567890def",
            is_current: true,
            user_agent:
              "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0",
            expires_at: isoPlusMin(60 * 24 * 7),
            kind: "web",
          },
        ],
      }),
    });
    vi.stubGlobal("fetch", fetchMock);
    try {
      render(<SessionsPanel />);
      await waitFor(() => screen.getByTestId("sessions-list"));
      // The current-device row renders with the pill, pinned first.
      expect(screen.getByTestId("current-session-pill")).toBeInTheDocument();
      const rows = screen.getAllByTestId(/^session-row/);
      expect(rows[0].getAttribute("data-testid")).toBe("session-row-current");
      expect(rows[0].textContent).toMatch(/Chrome on macOS/);
      // Its Revoke is disabled (sign out instead).
      expect(screen.getByTestId("revoke-button-disabled")).toBeDisabled();
      // The stale BFF row is still listed below as a revocable non-current.
      expect(screen.getAllByTestId("revoke-button")).toHaveLength(1);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("renders 'No other active sessions' when only the current session is returned", async () => {
    apiGet.mockResolvedValueOnce({
      sessions: [
        {
          id: { value: SID_CURRENT },
          user_agent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0",
          ip_address: "203.0.113.10",
          created_at: isoMinusMin(60),
          last_used_at: isoMinusMin(1),
          expires_at: isoPlusMin(60 * 24 * 7),
          is_current: true,
        },
      ],
    });
    render(<SessionsPanel />);
    await waitFor(() => screen.getByTestId("sessions-empty-state"));
    expect(screen.getByTestId("sessions-empty-state").textContent).toMatch(
      /No other active sessions/i,
    );
  });

  it("renders 5 rows with current pinned first + correct labels", async () => {
    apiGet.mockResolvedValueOnce({
      sessions: [
        // Intentionally NOT current first to assert the panel re-sorts.
        {
          id: { value: SID_OTHER1 },
          user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Firefox/121.0",
          ip_address: "198.51.100.7",
          created_at: isoMinusMin(60 * 24 * 2),
          last_used_at: isoMinusMin(30),
          expires_at: isoPlusMin(60 * 24 * 14),
          is_current: false,
        },
        {
          id: { value: SID_CURRENT },
          user_agent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0",
          ip_address: "203.0.113.10",
          created_at: isoMinusMin(60),
          last_used_at: isoMinusMin(1),
          expires_at: isoPlusMin(60 * 24 * 7),
          is_current: true,
        },
        {
          id: { value: SID_OTHER2 },
          user_agent: "iogridd/1.0.0",
          ip_address: "192.0.2.15",
          created_at: isoMinusMin(60 * 24 * 30),
          last_used_at: isoMinusMin(5),
          expires_at: isoPlusMin(60 * 24 * 90),
          is_current: false,
        },
        {
          id: { value: "44444444-4444-4444-4444-444444444444" },
          user_agent:
            "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0",
          ip_address: "192.0.2.16",
          created_at: isoMinusMin(60 * 24 * 7),
          last_used_at: isoMinusMin(60 * 6),
          expires_at: isoPlusMin(60 * 24 * 30),
          is_current: false,
        },
        {
          id: { value: "55555555-5555-5555-5555-555555555555" },
          user_agent: "curl/8.4.0",
          ip_address: "192.0.2.17",
          created_at: isoMinusMin(60 * 24 * 1),
          last_used_at: isoMinusMin(60 * 12),
          expires_at: isoPlusMin(60 * 24 * 2),
          is_current: false,
        },
      ],
    });
    render(<SessionsPanel />);

    await waitFor(() => screen.getByTestId("sessions-list"));
    const rows = screen
      .getAllByTestId(/^session-row/)
      .map((el) => el as HTMLElement);
    expect(rows).toHaveLength(5);
    // Current is first.
    expect(rows[0].getAttribute("data-testid")).toBe("session-row-current");
    expect(screen.getByTestId("current-session-pill")).toBeInTheDocument();
    // Current's Revoke is disabled.
    expect(screen.getByTestId("revoke-button-disabled")).toBeDisabled();
    // 4 enabled Revoke buttons (non-current rows).
    const enabled = screen.getAllByTestId("revoke-button");
    expect(enabled).toHaveLength(4);
    enabled.forEach((b) => expect(b).not.toBeDisabled());
    // UA humanisation.
    expect(rows[0].textContent).toMatch(/Chrome on macOS/);
    expect(rows[1].textContent).toMatch(/Firefox on Windows/);
    // IP + time labels.
    expect(rows[0].textContent).toContain("203.0.113.10");
    expect(rows[0].textContent).toMatch(/last active/);
  });

  it("revokes a non-current session via DELETE and re-fetches", async () => {
    apiGet.mockResolvedValueOnce({
      sessions: [
        {
          id: { value: SID_CURRENT },
          user_agent: "Mozilla/5.0 (Macintosh) Chrome/120.0",
          ip_address: "203.0.113.10",
          created_at: isoMinusMin(60),
          last_used_at: isoMinusMin(1),
          expires_at: isoPlusMin(60 * 24 * 7),
          is_current: true,
        },
        {
          id: { value: SID_OTHER1 },
          user_agent: "Mozilla/5.0 (Windows NT 10.0) Firefox/121.0",
          ip_address: "198.51.100.7",
          created_at: isoMinusMin(60 * 24 * 2),
          last_used_at: isoMinusMin(30),
          expires_at: isoPlusMin(60 * 24 * 14),
          is_current: false,
        },
      ],
    });
    // After revoke, only current remains.
    apiGet.mockResolvedValueOnce({
      sessions: [
        {
          id: { value: SID_CURRENT },
          user_agent: "Mozilla/5.0 (Macintosh) Chrome/120.0",
          ip_address: "203.0.113.10",
          created_at: isoMinusMin(60),
          last_used_at: isoMinusMin(1),
          expires_at: isoPlusMin(60 * 24 * 7),
          is_current: true,
        },
      ],
    });
    apiDel.mockResolvedValueOnce({ ok: true });

    render(<SessionsPanel />);
    await waitFor(() => screen.getByTestId("sessions-list"));

    const revokeButtons = screen.getAllByTestId("revoke-button");
    expect(revokeButtons).toHaveLength(1);
    fireEvent.click(revokeButtons[0]);

    await waitFor(() => expect(apiDel).toHaveBeenCalledTimes(1));
    expect(apiDel).toHaveBeenCalledWith(
      `/api/v1/account/sessions/${encodeURIComponent(SID_OTHER1)}`,
    );
    // After refresh, only current remains → empty-state.
    await waitFor(() => screen.getByTestId("sessions-empty-state"));
    expect(apiGet).toHaveBeenCalledTimes(2);
  });

  it("renders the error empty-state when the list call fails", async () => {
    apiGet.mockRejectedValueOnce(new Error("boom"));
    render(<SessionsPanel />);
    await waitFor(() => screen.getByTestId("sessions-empty-state"));
    expect(screen.getByTestId("sessions-empty-state").textContent).toMatch(
      /Could not load sessions/i,
    );
  });

  it("tolerates the legacy camelCase shape from older gateway-bff builds", async () => {
    apiGet.mockResolvedValueOnce({
      sessions: [
        {
          id: { value: SID_CURRENT },
          userAgent: "curl/8.4.0",
          ipAddress: "192.0.2.17",
          createdAt: isoMinusMin(60),
          lastSeenAt: isoMinusMin(2),
          current: true,
        },
        {
          id: { value: SID_OTHER1 },
          userAgent: "Mozilla/5.0 (Windows NT 10.0) Firefox/121.0",
          ipAddress: "198.51.100.7",
          createdAt: isoMinusMin(60 * 24),
          lastSeenAt: isoMinusMin(60),
          current: false,
        },
      ],
    });
    render(<SessionsPanel />);
    await waitFor(() => screen.getByTestId("sessions-list"));
    expect(screen.getByTestId("session-row-current")).toBeInTheDocument();
    expect(screen.getAllByTestId("revoke-button")).toHaveLength(1);
  });
});
