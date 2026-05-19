import type { Meta, StoryObj } from "@storybook/react";
import { BurnTimeseries } from "./BurnTimeseries";

const meta: Meta<typeof BurnTimeseries> = {
  title: "Wallet/BurnTimeseries",
  component: BurnTimeseries,
};
export default meta;
type Story = StoryObj<typeof BurnTimeseries>;

export const Empty: Story = { args: { data: [] } };

export const ThirtyDays: Story = {
  args: {
    data: Array.from({ length: 30 }, (_, i) => {
      const d = new Date(Date.parse("2026-04-20T00:00:00Z") + i * 86_400_000);
      return {
        date: d.toISOString().slice(0, 10),
        burnedUi: Math.round(500 + Math.sin(i / 3) * 200 + i * 25),
      };
    }),
  },
};
