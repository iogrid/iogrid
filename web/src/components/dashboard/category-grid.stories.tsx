import type { Meta, StoryObj } from "@storybook/react";
import * as React from "react";
import { CategoryGrid } from "./category-grid";
import { CATEGORIES } from "@/lib/categories";

const meta: Meta<typeof CategoryGrid> = {
  title: "Dashboard/CategoryGrid",
  component: CategoryGrid,
};

export default meta;
type Story = StoryObj<typeof CategoryGrid>;

function Wrapper({ initial }: { initial: string[] }) {
  const [sel, setSel] = React.useState(initial);
  return (
    <CategoryGrid
      categories={CATEGORIES}
      selected={sel}
      onToggle={(slug, on) =>
        setSel((s) => (on ? Array.from(new Set([...s, slug])) : s.filter((x) => x !== slug)))
      }
    />
  );
}

export const Empty: Story = { render: () => <Wrapper initial={[]} /> };
export const SomeSelected: Story = {
  render: () => <Wrapper initial={["e_commerce", "seo"]} />,
};
