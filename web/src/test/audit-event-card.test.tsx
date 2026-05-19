import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { AuditEventCard } from "@/components/dashboard/audit-event-card";
import type { AuditEvent } from "@/lib/types";

const baseEvent: AuditEvent = {
  kind: "EVENT_KIND_WORKLOAD_DISPATCHED",
  occurredAt: new Date().toISOString(),
  workloadType: "WORKLOAD_TYPE_BANDWIDTH",
  category: "e_commerce",
  customerDisplayName: "Acme Inc.",
  destinationSummary: "api.example.com",
  bytes: "1048576",
};

describe("AuditEventCard", () => {
  it("renders all the basic event metadata", () => {
    render(<AuditEventCard event={baseEvent} />);
    expect(screen.getByText("Workload dispatched")).toBeInTheDocument();
    expect(screen.getByTestId("audit-category")).toHaveTextContent("E Commerce");
    expect(screen.getByText("Acme Inc.")).toBeInTheDocument();
    expect(screen.getByText("api.example.com")).toBeInTheDocument();
    expect(screen.getByText(/transferred/)).toBeInTheDocument();
  });

  it("fires all three block handlers", () => {
    const blockCat = vi.fn();
    const blockCust = vi.fn();
    const blockDest = vi.fn();
    render(
      <AuditEventCard
        event={baseEvent}
        onBlockCategory={blockCat}
        onBlockCustomer={blockCust}
        onBlockDestination={blockDest}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /block category/i }));
    fireEvent.click(screen.getByRole("button", { name: /block customer/i }));
    fireEvent.click(screen.getByRole("button", { name: /block destination/i }));
    expect(blockCat).toHaveBeenCalledWith("e_commerce");
    expect(blockCust).toHaveBeenCalledWith("Acme Inc.");
    expect(blockDest).toHaveBeenCalledWith("api.example.com");
  });

  it("applies the rose accent on blocked events", () => {
    const { container } = render(
      <AuditEventCard
        event={{ ...baseEvent, kind: "EVENT_KIND_WORKLOAD_BLOCKED" }}
      />,
    );
    const card = container.querySelector('[data-testid="audit-event-card"]');
    expect(card?.className).toContain("rose");
  });
});
