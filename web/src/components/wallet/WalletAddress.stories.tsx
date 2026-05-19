import type { Meta, StoryObj } from "@storybook/react";
import { WalletAddress } from "./WalletAddress";

const meta: Meta<typeof WalletAddress> = {
  title: "Wallet/WalletAddress",
  component: WalletAddress,
};
export default meta;
type Story = StoryObj<typeof WalletAddress>;

const ADDR = "DhKZNz4u7TaqfaWvVy7Ldd5xKzN6m8aaaa1234567890";

export const Truncated: Story = {
  args: { address: ADDR },
};

export const Full: Story = {
  args: { address: ADDR, truncate: false },
};

export const LongerHeadTail: Story = {
  args: { address: ADDR, head: 8, tail: 6 },
};
