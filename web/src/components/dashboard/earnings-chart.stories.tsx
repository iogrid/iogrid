import type { Meta, StoryObj } from "@storybook/react";
import { EarningsChart } from "./earnings-chart";

const meta: Meta<typeof EarningsChart> = {
  title: "Dashboard/EarningsChart",
  component: EarningsChart,
};

export default meta;
type Story = StoryObj<typeof EarningsChart>;

export const Empty: Story = { args: { data: [] } };

export const FourteenDays: Story = {
  args: {
    data: Array.from({ length: 14 }, (_, i) => ({
      bucket: `5/${i + 1}`,
      amount: Math.round(Math.random() * 12 + i * 1.2),
    })),
  },
};
