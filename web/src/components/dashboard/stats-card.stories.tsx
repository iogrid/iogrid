import type { Meta, StoryObj } from "@storybook/react";
import { StatsCard } from "./stats-card";

const meta: Meta<typeof StatsCard> = {
  title: "Dashboard/StatsCard",
  component: StatsCard,
};

export default meta;
type Story = StoryObj<typeof StatsCard>;

export const Plain: Story = {
  args: {
    label: "Earnings this month",
    value: "$42.18",
    hint: "May 2026",
  },
};

export const WithDelta: Story = {
  args: {
    label: "Bandwidth",
    value: "12.4 GB",
    hint: "of 50 GB cap",
    delta: { value: "+18%", direction: "up" },
  },
};

export const WithSparkline: Story = {
  args: {
    label: "Daily revenue",
    value: "$8.90",
    hint: "Last 14 days",
    series: [3, 4, 6, 4, 7, 8, 9, 6, 10, 11, 12, 14, 13, 17],
  },
};
