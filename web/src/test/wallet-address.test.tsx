import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { WalletAddress } from "@/components/wallet/WalletAddress";

const ADDR = "DhKZNz4u7TaqfaWvVy7Ldd5xKzN6m8aaaa1234567890";

describe("WalletAddress", () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it("renders a truncated form by default", () => {
    render(<WalletAddress address={ADDR} />);
    const btn = screen.getByTestId("wallet-address");
    expect(btn.textContent).toContain("…");
    expect(btn.textContent).not.toContain(ADDR);
  });

  it("renders the full address when truncate=false", () => {
    render(<WalletAddress address={ADDR} truncate={false} />);
    expect(screen.getByTestId("wallet-address").textContent).toContain(ADDR);
  });

  it("copies the address on click", async () => {
    render(<WalletAddress address={ADDR} />);
    fireEvent.click(screen.getByTestId("wallet-address"));
    await waitFor(() => {
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(ADDR);
    });
  });
});
