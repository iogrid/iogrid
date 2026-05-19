import type { Meta, StoryObj } from "@storybook/react";
import * as React from "react";
import { ScheduleCalendar } from "./schedule-calendar";

const meta: Meta<typeof ScheduleCalendar> = {
  title: "Dashboard/ScheduleCalendar",
  component: ScheduleCalendar,
};

export default meta;
type Story = StoryObj<typeof ScheduleCalendar>;

function Wrapper({ initial }: { initial: number[] }) {
  const [v, setV] = React.useState(initial);
  return <ScheduleCalendar value={v} onChange={setV} />;
}

export const AlwaysOn: Story = {
  render: () => <Wrapper initial={new Array(7).fill(0xffffff)} />,
};

export const WorkHours: Story = {
  render: () => {
    const mask = 0;
    const work = (0xff << 8) | (0xff << 16); // 9..23
    const masks = [
      work,
      work,
      work,
      work,
      work,
      mask,
      mask,
    ];
    return <Wrapper initial={masks} />;
  },
};
