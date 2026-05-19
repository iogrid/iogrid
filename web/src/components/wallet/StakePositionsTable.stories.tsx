import type { Meta, StoryObj } from "@storybook/react";
import { StakePositionsTable } from "./StakePositionsTable";

const meta: Meta<typeof StakePositionsTable> = {
  title: "Wallet/StakePositionsTable",
  component: StakePositionsTable,
};
export default meta;
type Story = StoryObj<typeof StakePositionsTable>;

export const Empty: Story = {
  args: {
    positions: [],
    onClaim: async () => {},
    onEarlyUnlock: async () => {},
  },
};

export const WithPositions: Story = {
  args: {
    positions: [
      {
        id: "a1b2c3d4e5f6",
        amount: "1000000000000",
        amountUi: 1000,
        lockPeriodDays: 90,
        tierMultiplier: 1.25,
        openedAt: "2026-04-01T00:00:00Z",
        unlocksAt: "2026-07-01T00:00:00Z",
        accruedYieldUi: 12.5,
        unlocked: false,
      },
      {
        id: "ff112233aabb",
        amount: "500000000000",
        amountUi: 500,
        lockPeriodDays: 30,
        tierMultiplier: 1.0,
        openedAt: "2026-04-10T00:00:00Z",
        unlocksAt: "2026-05-10T00:00:00Z",
        accruedYieldUi: 2.1,
        unlocked: true,
      },
    ],
    onClaim: async () => {},
    onEarlyUnlock: async () => {},
  },
};
