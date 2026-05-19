import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import {
  ScheduleCalendar,
  bitmasksToCalendarWindows,
  calendarWindowsToBitmasks,
} from "@/components/dashboard/schedule-calendar";

describe("ScheduleCalendar", () => {
  it("renders 7 rows × 24 toggleable cells = 168 buttons", () => {
    render(<ScheduleCalendar value={new Array(7).fill(0)} onChange={() => {}} />);
    expect(screen.getAllByRole("button")).toHaveLength(7 * 24);
  });

  it("toggles a cell via mousedown", () => {
    const onChange = vi.fn();
    render(<ScheduleCalendar value={new Array(7).fill(0)} onChange={onChange} />);
    // Mon 0:00 — first button.
    fireEvent.mouseDown(screen.getAllByRole("button")[0]);
    expect(onChange).toHaveBeenCalled();
    const next = onChange.mock.calls[0][0] as number[];
    expect(next[0] & 1).toBe(1);
  });
});

describe("calendarWindowsToBitmasks", () => {
  it("returns full-week availability when given no windows", () => {
    const masks = calendarWindowsToBitmasks([]);
    expect(masks).toHaveLength(7);
    expect(masks.every((m) => m === 0xffffff)).toBe(true);
  });

  it("packs a single Mon 9-17 window into the right bits", () => {
    const masks = calendarWindowsToBitmasks([
      {
        dayOfWeek: "DAY_OF_WEEK_MONDAY",
        startLocalTime: "09:00",
        endLocalTime: "17:00",
      },
    ]);
    // Bits 9..16 set on day 0.
    expect(masks[0] & (1 << 9)).toBeTruthy();
    expect(masks[0] & (1 << 16)).toBeTruthy();
    expect(masks[0] & (1 << 17)).toBeFalsy();
  });
});

describe("bitmasksToCalendarWindows", () => {
  it("collapses contiguous bits into half-open intervals", () => {
    const masks = new Array(7).fill(0);
    // Wed 10..14
    masks[2] = (1 << 10) | (1 << 11) | (1 << 12) | (1 << 13);
    const out = bitmasksToCalendarWindows(masks, "UTC");
    expect(out).toEqual([
      {
        dayOfWeek: "DAY_OF_WEEK_WEDNESDAY",
        startLocalTime: "10:00",
        endLocalTime: "14:00",
        timezone: "UTC",
      },
    ]);
  });

  it("survives a round-trip via calendarWindowsToBitmasks", () => {
    const masks = new Array(7).fill(0);
    masks[1] = 0xff00; // Tue 8..16
    masks[5] = 0x0fffff; // Sat 0..20
    const windows = bitmasksToCalendarWindows(masks, "Europe/Berlin");
    const round = calendarWindowsToBitmasks(windows);
    expect(round[1]).toBe(masks[1]);
    expect(round[5]).toBe(masks[5]);
  });
});
