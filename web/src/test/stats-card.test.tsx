import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { StatsCard } from "@/components/dashboard/stats-card";

describe("StatsCard", () => {
  it("renders the label, value and hint", () => {
    render(<StatsCard label="Earnings" value="$42" hint="this month" />);
    expect(screen.getByText("Earnings")).toBeInTheDocument();
    expect(screen.getByText("$42")).toBeInTheDocument();
    expect(screen.getByText("this month")).toBeInTheDocument();
  });

  it("renders the delta pill when provided", () => {
    render(
      <StatsCard
        label="Bandwidth"
        value="12 GB"
        delta={{ value: "+18%", direction: "up" }}
      />,
    );
    expect(screen.getByText("↑ +18%")).toBeInTheDocument();
  });

  it("renders a sparkline svg when given a series", () => {
    const { container } = render(
      <StatsCard
        label="Trend"
        value="ok"
        series={[1, 2, 3, 4]}
      />,
    );
    expect(container.querySelector("svg")).toBeTruthy();
    expect(container.querySelector("polyline")).toBeTruthy();
  });
});
