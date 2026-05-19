import type { Meta, StoryObj } from "@storybook/react";
import { StakeForm } from "./StakeForm";

const meta: Meta<typeof StakeForm> = {
  title: "Wallet/StakeForm",
  component: StakeForm,
};
export default meta;
type Story = StoryObj<typeof StakeForm>;

export const Empty: Story = {
  args: {
    availableGrid: 0,
    onSubmit: async () => {},
  },
};

export const WithBalance: Story = {
  args: {
    availableGrid: 1250,
    onSubmit: async () => {},
  },
};
