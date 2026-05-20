/**
 * Vitest coverage for /account/wallets (issue #326).
 *
 * Pins:
 *   - GET /api/v1/account/wallets returns an empty list → renders the
 *     "No wallets bound yet" empty state (not a 404 banner).
 *   - GET returns one wallet → renders the row + truncated address +
 *     "Bound X ago" + Unbind button.
 *   - Unbind button fires DELETE /api/v1/account/wallets/{address}
 *     then re-fetches the list.
 *   - Failed GET surfaces a `wallets-load-error` element instead of
 *     wedging on "Loading…" forever.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

import { WalletsView } from "@/app/account/wallets/view";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const apiGet = vi.fn();
const apiPost = vi.fn();
const apiDel = vi.fn();
vi.mock("@/lib/api", () => ({
  browserApi: () => ({ get: apiGet, post: apiPost, del: apiDel }),
}));

// WalletConnectButton / WalletBalance / WalletBindFlow pull in
// @solana/wallet-adapter, which carries window-only globals that
// jsdom does not provide. Replace them with inert stubs so the view's
// own loading + error + list logic can be exercised in isolation.
vi.mock("@/components/wallet/WalletConnectButton", () => ({
  WalletConnectButton: () => <button>Connect</button>,
}));
vi.mock("@/components/wallet/WalletBalance", () => ({
  WalletBalance: () => <div data-testid="wallet-balance-stub" />,
}));
vi.mock("@/components/wallet/WalletBindFlow", () => ({
  WalletBindFlow: () => <button data-testid="wallet-bind-stub">Connect & bind wallet</button>,
}));
vi.mock("@/components/wallet/WalletAddress", () => ({
  WalletAddress: ({ address }: { address: string }) => (
    <span data-testid="wallet-address">{address}</span>
  ),
}));

const ADDR_1 = "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin";

describe("WalletsView", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    apiDel.mockReset();
  });

  it("renders the 'No wallets bound yet' empty state when GET returns an empty list", async () => {
    apiGet.mockResolvedValueOnce({ wallets: [] });
    render(<WalletsView />);
    await waitFor(() => screen.getByTestId("wallets-empty"));
    expect(screen.getByTestId("wallets-empty").textContent).toMatch(
      /No wallets bound yet/i,
    );
    // Empty state must NOT also render the load-error banner.
    expect(screen.queryByTestId("wallets-load-error")).toBeNull();
  });

  it("renders one row per bound wallet with the address + Unbind button", async () => {
    apiGet.mockResolvedValueOnce({
      wallets: [
        {
          walletAddress: ADDR_1,
          chain: "solana",
          boundAt: new Date(Date.now() - 60 * 1000).toISOString(),
        },
      ],
    });
    render(<WalletsView />);
    await waitFor(() => screen.getByTestId("wallets-list"));
    expect(screen.getByTestId("wallet-address").textContent).toBe(ADDR_1);
    expect(
      screen.getByRole("button", { name: /Unbind wallet/i }),
    ).toBeInTheDocument();
  });

  it("fires DELETE /api/v1/account/wallets/{address} on Unbind click and refreshes", async () => {
    apiGet.mockResolvedValueOnce({
      wallets: [
        {
          walletAddress: ADDR_1,
          chain: "solana",
          boundAt: new Date(Date.now() - 30 * 1000).toISOString(),
        },
      ],
    });
    apiDel.mockResolvedValueOnce(undefined);
    apiGet.mockResolvedValueOnce({ wallets: [] }); // post-unbind refresh

    render(<WalletsView />);
    await waitFor(() => screen.getByTestId("wallets-list"));
    fireEvent.click(screen.getByTestId("wallet-unbind-button"));

    await waitFor(() => screen.getByTestId("wallets-empty"));
    expect(apiDel).toHaveBeenCalledWith(
      `/api/v1/account/wallets/${encodeURIComponent(ADDR_1)}`,
    );
    expect(apiGet).toHaveBeenCalledTimes(2);
  });

  it("surfaces a load-error banner when GET fails (distinct from empty state)", async () => {
    apiGet.mockRejectedValueOnce(new Error("network is down"));
    render(<WalletsView />);
    await waitFor(() => screen.getByTestId("wallets-load-error"));
    expect(screen.getByTestId("wallets-load-error").textContent).toMatch(
      /Couldn.?t load wallets/i,
    );
    expect(screen.getByTestId("wallets-load-error").textContent).toMatch(
      /network is down/,
    );
    // The error banner must NOT also render the empty state (#322 pattern).
    expect(screen.queryByTestId("wallets-empty")).toBeNull();
  });
});
