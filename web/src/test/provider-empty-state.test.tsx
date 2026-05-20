import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import {
  ProviderEmptyState,
  PROVIDER_EMPTY_OVERVIEW_SUBTITLE,
  PROVIDER_EMPTY_EARNINGS_SUBTITLE,
  PROVIDER_EMPTY_SCHEDULE_SUBTITLE,
  PROVIDER_EMPTY_AUDIT_SUBTITLE,
  PROVIDER_EMPTY_STAKING_SUBTITLE,
} from "@/components/dashboard/provider-empty-state";

describe("ProviderEmptyState", () => {
  it("renders the canonical headline + CTA link to /install", () => {
    render(<ProviderEmptyState subtitle={PROVIDER_EMPTY_OVERVIEW_SUBTITLE} />);
    expect(
      screen.getByText(/You don['’]t have any provider machines paired yet\./),
    ).toBeInTheDocument();
    const cta = screen.getByRole("link", { name: /Install daemon/i });
    expect(cta).toHaveAttribute("href", "/install");
  });

  it("renders the supplied subtitle verbatim", () => {
    render(<ProviderEmptyState subtitle={PROVIDER_EMPTY_EARNINGS_SUBTITLE} />);
    expect(screen.getByText(PROVIDER_EMPTY_EARNINGS_SUBTITLE)).toBeInTheDocument();
  });

  it("exposes a stable test id for Playwright walks", () => {
    render(<ProviderEmptyState subtitle={PROVIDER_EMPTY_SCHEDULE_SUBTITLE} />);
    expect(screen.getByTestId("provider-empty-state")).toBeInTheDocument();
  });

  it("accepts a custom testId for surface-specific selectors", () => {
    render(
      <ProviderEmptyState
        subtitle={PROVIDER_EMPTY_AUDIT_SUBTITLE}
        testId="provide-audit-empty-state"
      />,
    );
    expect(
      screen.getByTestId("provide-audit-empty-state"),
    ).toBeInTheDocument();
  });

  it("ships each surface's canonical subtitle constant", () => {
    // Pin the copy so an accidental change in one surface doesn't drift
    // away from the catalog (#313).
    expect(PROVIDER_EMPTY_OVERVIEW_SUBTITLE).toMatch(/Install the iogrid daemon/);
    expect(PROVIDER_EMPTY_EARNINGS_SUBTITLE).toMatch(/Earnings will appear/);
    expect(PROVIDER_EMPTY_SCHEDULE_SUBTITLE).toMatch(/Pair a provider first/);
    expect(PROVIDER_EMPTY_AUDIT_SUBTITLE).toMatch(/Audit events will appear/);
    expect(PROVIDER_EMPTY_STAKING_SUBTITLE).toMatch(/Stake \$GRID/);
  });
});
