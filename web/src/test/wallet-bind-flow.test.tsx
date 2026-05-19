import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { ApiClient } from "@/lib/api";

// Mock the wallet-adapter hooks BEFORE the component import below so
// the component picks up the mocked module. `vi.mock()` is hoisted.
const mockSignMessage = vi.fn();
const mockSetVisible = vi.fn();
const mockUseWallet = vi.fn();

vi.mock("@solana/wallet-adapter-react", () => ({
  useWallet: () => mockUseWallet(),
}));

vi.mock("@solana/wallet-adapter-react-ui", () => ({
  useWalletModal: () => ({ setVisible: mockSetVisible }),
}));

// Use require-style dynamic import after mocks are registered.
import { WalletBindFlow } from "@/components/wallet/WalletBindFlow";

const PUBKEY = {
  toBase58: () => "DhKZNz4u7TaqfaWvVy7Ldd5xKzN6m8aaaa1234567890",
};

function clientWith(handler: (url: string, init: RequestInit) => Response): ApiClient {
  const fetcher = vi
    .fn<typeof fetch>()
    .mockImplementation(async (input, init) => {
      const url = typeof input === "string" ? input : (input as URL).toString();
      return handler(url, init ?? {});
    });
  return new ApiClient({
    baseUrl: "https://test.example",
    fetcher: fetcher as unknown as typeof fetch,
  });
}

describe("WalletBindFlow", () => {
  beforeEach(() => {
    mockSignMessage.mockReset();
    mockSetVisible.mockReset();
    mockUseWallet.mockReset();
  });

  it("opens the wallet-adapter modal when no wallet is connected", () => {
    mockUseWallet.mockReturnValue({
      connected: false,
      publicKey: null,
      signMessage: undefined,
    });
    render(<WalletBindFlow />);
    fireEvent.click(screen.getByTestId("wallet-bind-button"));
    expect(mockSetVisible).toHaveBeenCalledWith(true);
  });

  it("runs the SIWS handshake end-to-end when connected", async () => {
    mockSignMessage.mockResolvedValue(new Uint8Array([1, 2, 3, 4]));
    mockUseWallet.mockReturnValue({
      connected: true,
      publicKey: PUBKEY,
      signMessage: mockSignMessage,
    });

    const onBound = vi.fn();
    let captured: Record<string, unknown> | null = null;
    const client = clientWith((url, init) => {
      if (url.endsWith("/start-binding")) {
        return new Response(
          JSON.stringify({
            nonce: "n",
            challenge: "Sign this: n",
            expiresAt: "2026-05-19T00:00:00Z",
          }),
          { status: 200 },
        );
      }
      if (url.endsWith("/complete-binding")) {
        captured = JSON.parse(String(init.body));
        return new Response(
          JSON.stringify({
            walletAddress: PUBKEY.toBase58(),
            chain: "solana",
            boundAt: "2026-05-19T00:00:00Z",
          }),
          { status: 200 },
        );
      }
      return new Response(null, { status: 404 });
    });

    render(<WalletBindFlow apiClient={client} onBound={onBound} />);
    fireEvent.click(screen.getByTestId("wallet-bind-button"));

    await waitFor(() => {
      expect(mockSignMessage).toHaveBeenCalled();
      expect(onBound).toHaveBeenCalled();
    });
    // The signature is base58 of [1,2,3,4]
    expect(captured).not.toBeNull();
    const c = captured as unknown as {
      walletAddress: string;
      nonce: string;
      signature: string;
    };
    expect(c.walletAddress).toBe(PUBKEY.toBase58());
    expect(c.nonce).toBe("n");
    expect(c.signature).toMatch(/^[A-HJ-NP-Za-km-z1-9]+$/);
  });

  it("surfaces an error if the start-binding call fails", async () => {
    mockUseWallet.mockReturnValue({
      connected: true,
      publicKey: PUBKEY,
      signMessage: mockSignMessage,
    });
    const client = clientWith(() => {
      return new Response(JSON.stringify({ code: "bad", message: "no" }), {
        status: 400,
      });
    });
    render(<WalletBindFlow apiClient={client} />);
    fireEvent.click(screen.getByTestId("wallet-bind-button"));
    await waitFor(() => {
      expect(screen.getByTestId("wallet-bind-error")).toBeInTheDocument();
    });
  });
});
