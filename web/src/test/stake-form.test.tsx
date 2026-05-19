import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { StakeForm } from "@/components/wallet/StakeForm";

describe("StakeForm", () => {
  it("renders the four lock-period tiers", () => {
    render(<StakeForm availableGrid={100} onSubmit={vi.fn()} />);
    expect(screen.getByTestId("stake-period-30")).toBeInTheDocument();
    expect(screen.getByTestId("stake-period-90")).toBeInTheDocument();
    expect(screen.getByTestId("stake-period-180")).toBeInTheDocument();
    expect(screen.getByTestId("stake-period-365")).toBeInTheDocument();
  });

  it("updates the multiplier preview when a tier is selected", () => {
    render(<StakeForm availableGrid={100} onSubmit={vi.fn()} />);
    const amountInput = screen.getByTestId("stake-amount-input") as HTMLInputElement;
    fireEvent.change(amountInput, { target: { value: "10" } });
    // default tier = 30 → 1.0× → preview = 10
    expect(screen.getByTestId("stake-preview").textContent).toContain("10");

    // switch to 365d → 2.0× → preview = 20
    fireEvent.click(screen.getByTestId("stake-period-365"));
    expect(screen.getByTestId("stake-preview").textContent).toContain("20");
  });

  it("rejects negative or zero amounts", async () => {
    const onSubmit = vi.fn();
    render(<StakeForm availableGrid={100} onSubmit={onSubmit} />);
    fireEvent.change(screen.getByTestId("stake-amount-input"), {
      target: { value: "0" },
    });
    fireEvent.click(screen.getByTestId("stake-submit-button"));
    await waitFor(() => {
      expect(screen.getByTestId("stake-form-error")).toBeInTheDocument();
    });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("rejects amounts exceeding the available balance", async () => {
    const onSubmit = vi.fn();
    render(<StakeForm availableGrid={5} onSubmit={onSubmit} />);
    fireEvent.change(screen.getByTestId("stake-amount-input"), {
      target: { value: "10" },
    });
    fireEvent.click(screen.getByTestId("stake-submit-button"));
    await waitFor(() => {
      expect(screen.getByTestId("stake-form-error").textContent).toMatch(
        /exceeds/,
      );
    });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("submits a valid amount and selected period", async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<StakeForm availableGrid={100} onSubmit={onSubmit} />);
    fireEvent.change(screen.getByTestId("stake-amount-input"), {
      target: { value: "42" },
    });
    fireEvent.click(screen.getByTestId("stake-period-180"));
    fireEvent.click(screen.getByTestId("stake-submit-button"));
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith("42", 180);
    });
  });
});
