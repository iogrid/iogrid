import type { Meta, StoryObj } from "@storybook/react";
import { AuditEventCard } from "./audit-event-card";
import type { AuditEvent } from "@/lib/types";

const meta: Meta<typeof AuditEventCard> = {
  title: "Dashboard/AuditEventCard",
  component: AuditEventCard,
};

export default meta;
type Story = StoryObj<typeof AuditEventCard>;

const base: AuditEvent = {
  kind: "EVENT_KIND_WORKLOAD_DISPATCHED",
  occurredAt: new Date(Date.now() - 12_000).toISOString(),
  workloadType: "WORKLOAD_TYPE_BANDWIDTH",
  category: "e_commerce",
  customerDisplayName: "Acme Inc.",
  destinationSummary: "api.example.com",
  bytes: "1572864",
};

export const Dispatched: Story = { args: { event: base } };

export const Blocked: Story = {
  args: {
    event: {
      ...base,
      kind: "EVENT_KIND_WORKLOAD_BLOCKED",
      bytes: "0",
    },
  },
};

export const EarningsCredited: Story = {
  args: {
    event: {
      ...base,
      kind: "EVENT_KIND_EARNINGS_CREDITED",
      destinationSummary: "",
      bytes: "0",
    },
  },
};

export const WithBlockActions: Story = {
  args: {
    event: base,
    onBlockCategory: () => {},
    onBlockCustomer: () => {},
    onBlockDestination: () => {},
  },
};
