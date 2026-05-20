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

  /**
   * Regression for #314: gateway-bff emits proto enums as numeric tags
   * via encoding/json. The card must still render the right label and
   * accent when `kind` arrives as a number.
   */
  it("accepts numeric proto-tag wire form for kind", () => {
    const { container } = render(
      <AuditEventCard event={{ ...baseEvent, kind: 3 as unknown as number }} />,
    );
    expect(screen.getByText("Workload blocked")).toBeInTheDocument();
    const card = container.querySelector('[data-testid="audit-event-card"]');
    expect(card?.className).toContain("rose");
    expect(card?.getAttribute("data-kind")).toBe("EVENT_KIND_WORKLOAD_BLOCKED");
  });

  /**
   * Regression for #319: the body MUST NOT contain the "Customer-X"
   * placeholder under any circumstance. Older versions of this card
   * fell back to it for non-customer event kinds and for customer
   * events with no display name; both cases now route through the
   * discriminated renderer.
   */
  describe("#319 — no phantom Customer-X placeholder", () => {
    it("does not render Customer-X on a customer event missing the display name", () => {
      const { container } = render(
        <AuditEventCard
          event={{ ...baseEvent, customerDisplayName: "" }}
        />,
      );
      expect(container.textContent ?? "").not.toContain("Customer-X");
      // The diagnostic affordance fires instead.
      expect(screen.getByTestId("audit-unknown-customer")).toBeInTheDocument();
      expect(screen.getByText(/unknown customer/i)).toBeInTheDocument();
    });

    it("renders a kind-specific row for SCHEDULER_TRANSITION with from/to metadata", () => {
      const { container } = render(
        <AuditEventCard
          event={{
            ...baseEvent,
            kind: "EVENT_KIND_SCHEDULER_TRANSITION",
            customerDisplayName: "",
            destinationSummary: "",
            category: "",
            metadata: {
              from: "SCHEDULER_STATE_ACTIVE",
              to: "SCHEDULER_STATE_PAUSED_USER_ACTIVE",
            },
          }}
        />,
      );
      expect(container.textContent ?? "").not.toContain("Customer-X");
      expect(screen.getByText("Scheduler change")).toBeInTheDocument();
      expect(screen.getByText("SCHEDULER_STATE_ACTIVE")).toBeInTheDocument();
      expect(
        screen.getByText("SCHEDULER_STATE_PAUSED_USER_ACTIVE"),
      ).toBeInTheDocument();
      // Category pill must NOT render for non-customer kinds (no
      // category attached to a scheduler transition).
      expect(screen.queryByTestId("audit-category")).not.toBeInTheDocument();
    });

    it("renders a kind-specific row for SCHEDULER_TRANSITION without metadata", () => {
      const { container } = render(
        <AuditEventCard
          event={{
            ...baseEvent,
            kind: "EVENT_KIND_SCHEDULER_TRANSITION",
            customerDisplayName: "",
            destinationSummary: "",
            category: "",
          }}
        />,
      );
      expect(container.textContent ?? "").not.toContain("Customer-X");
      expect(screen.getByText(/scheduler state changed/i)).toBeInTheDocument();
    });

    it("renders a debug row for stray KEEPALIVE events", () => {
      const { container } = render(
        <AuditEventCard
          event={{
            ...baseEvent,
            kind: "EVENT_KIND_KEEPALIVE",
            customerDisplayName: "",
            destinationSummary: "",
            category: "",
          }}
        />,
      );
      expect(container.textContent ?? "").not.toContain("Customer-X");
      expect(screen.getByText(/stream keep-alive/i)).toBeInTheDocument();
    });

    it("hides the per-customer action buttons on non-customer events", () => {
      render(
        <AuditEventCard
          event={{
            ...baseEvent,
            kind: "EVENT_KIND_SCHEDULER_TRANSITION",
            customerDisplayName: "",
            destinationSummary: "",
            category: "",
          }}
          onBlockCategory={() => {}}
          onBlockCustomer={() => {}}
          onBlockDestination={() => {}}
        />,
      );
      expect(
        screen.queryByRole("button", { name: /block category/i }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /block customer/i }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: /block destination/i }),
      ).not.toBeInTheDocument();
    });

    it("renders an honest fallback for an unknown numeric kind without inventing a customer", () => {
      const { container } = render(
        <AuditEventCard
          event={{
            ...baseEvent,
            // 99 is well past the proto's defined enum range.
            kind: 99 as unknown as number,
            customerDisplayName: "",
            destinationSummary: "",
            category: "",
            metadata: { reason: "experimental" },
          }}
        />,
      );
      expect(container.textContent ?? "").not.toContain("Customer-X");
      expect(screen.getByText("Event")).toBeInTheDocument();
      expect(screen.getByText(/reason=experimental/)).toBeInTheDocument();
    });
  });
});
