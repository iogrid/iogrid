"use client";

import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * ScheduleCalendar — a weekly 7×24 grid editor. Each cell is one hour;
 * clicking (or click-dragging) flips its enabled state. The parent owns
 * the canonical state and gets called back via `onChange`.
 *
 * Internal model: a 7-element array of 24-bit masks (bit i = enabled at
 * hour i, local time). This keeps the state cheap to diff against the
 * proto CalendarSchedule.windows list at save time.
 */
export interface ScheduleCalendarProps {
  /** Bitmask per day-of-week, ordered Mon..Sun. */
  value: number[];
  onChange: (next: number[]) => void;
  /** Disable interaction (e.g. while saving). */
  disabled?: boolean;
}

const DAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

export function ScheduleCalendar({
  value,
  onChange,
  disabled = false,
}: ScheduleCalendarProps) {
  const [dragging, setDragging] = React.useState<"on" | "off" | null>(null);

  const safeValue = React.useMemo(() => {
    const out = new Array<number>(7).fill(0);
    for (let i = 0; i < 7; i++) {
      out[i] = value[i] ?? 0;
    }
    return out;
  }, [value]);

  const set = (day: number, hour: number, on: boolean) => {
    if (disabled) return;
    const next = safeValue.slice();
    const mask = 1 << hour;
    next[day] = on ? next[day] | mask : next[day] & ~mask;
    onChange(next);
  };

  const onDown = (day: number, hour: number) => {
    if (disabled) return;
    const isOn = ((safeValue[day] >> hour) & 1) === 1;
    setDragging(isOn ? "off" : "on");
    set(day, hour, !isOn);
  };
  const onEnter = (day: number, hour: number) => {
    if (!dragging) return;
    set(day, hour, dragging === "on");
  };

  return (
    <div
      data-testid="schedule-calendar"
      className="overflow-x-auto rounded-md border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900"
      onMouseUp={() => setDragging(null)}
      onMouseLeave={() => setDragging(null)}
    >
      <table className="w-full border-collapse text-[10px]">
        <thead>
          <tr>
            <th className="w-12" aria-hidden />
            {Array.from({ length: 24 }, (_, h) => (
              <th
                key={h}
                className="px-0 text-center font-normal text-zinc-500"
                aria-label={`Hour ${h}`}
              >
                {h % 6 === 0 ? h : ""}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {DAYS.map((day, di) => (
            <tr key={day}>
              <th
                scope="row"
                className="pr-2 text-right text-xs font-medium text-zinc-600 dark:text-zinc-400"
              >
                {day}
              </th>
              {Array.from({ length: 24 }, (_, hour) => {
                const on = ((safeValue[di] >> hour) & 1) === 1;
                return (
                  <td key={hour} className="p-[1px]">
                    <button
                      type="button"
                      disabled={disabled}
                      onMouseDown={() => onDown(di, hour)}
                      onMouseEnter={() => onEnter(di, hour)}
                      aria-pressed={on}
                      aria-label={`${day} ${hour}:00 ${on ? "enabled" : "disabled"}`}
                      className={cn(
                        "h-5 w-full rounded-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-zinc-500",
                        on
                          ? "bg-emerald-500 hover:bg-emerald-400"
                          : "bg-zinc-200 hover:bg-zinc-300 dark:bg-zinc-800 dark:hover:bg-zinc-700",
                        disabled && "opacity-50",
                      )}
                    />
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
      <p className="mt-2 text-xs text-zinc-500">
        Click and drag to enable hours. Green = available for workloads.
      </p>
    </div>
  );
}

/**
 * Helper used by the schedule page to translate a list of
 * CalendarWindow entries (proto wire format) into the 7×24 bitmask the
 * calendar component renders.
 *
 * The proto stores windows as half-open intervals (start inclusive, end
 * exclusive) of local-time strings ("09:00"..."17:00") plus a
 * day-of-week enum. We collapse them into a per-day 24-bit mask.
 */
export function calendarWindowsToBitmasks(
  windows: Array<{
    dayOfWeek?: string;
    startLocalTime: string;
    endLocalTime: string;
  }>,
): number[] {
  const masks = new Array<number>(7).fill(0);
  if (!windows || windows.length === 0) {
    // Default: all hours allowed (full availability).
    return masks.map(() => 0xffffff);
  }
  for (const w of windows) {
    const day = dayOfWeekIndex(w.dayOfWeek);
    if (day < 0) continue;
    const start = hourFromLocalTime(w.startLocalTime);
    const end = hourFromLocalTime(w.endLocalTime);
    if (start === null || end === null) continue;
    for (let h = start; h < end; h++) {
      masks[day] |= 1 << h;
    }
  }
  return masks;
}

export function bitmasksToCalendarWindows(
  masks: number[],
  timezone: string,
): Array<{ dayOfWeek: string; startLocalTime: string; endLocalTime: string; timezone: string }> {
  const days = [
    "DAY_OF_WEEK_MONDAY",
    "DAY_OF_WEEK_TUESDAY",
    "DAY_OF_WEEK_WEDNESDAY",
    "DAY_OF_WEEK_THURSDAY",
    "DAY_OF_WEEK_FRIDAY",
    "DAY_OF_WEEK_SATURDAY",
    "DAY_OF_WEEK_SUNDAY",
  ];
  const out: Array<{
    dayOfWeek: string;
    startLocalTime: string;
    endLocalTime: string;
    timezone: string;
  }> = [];
  for (let d = 0; d < 7; d++) {
    let h = 0;
    while (h < 24) {
      const on = ((masks[d] >> h) & 1) === 1;
      if (!on) {
        h += 1;
        continue;
      }
      const start = h;
      while (h < 24 && ((masks[d] >> h) & 1) === 1) h += 1;
      out.push({
        dayOfWeek: days[d],
        startLocalTime: `${String(start).padStart(2, "0")}:00`,
        endLocalTime: `${String(h).padStart(2, "0")}:00`,
        timezone,
      });
    }
  }
  return out;
}

function dayOfWeekIndex(label: string | undefined): number {
  switch ((label ?? "").toUpperCase()) {
    case "DAY_OF_WEEK_MONDAY":
    case "MONDAY":
      return 0;
    case "DAY_OF_WEEK_TUESDAY":
    case "TUESDAY":
      return 1;
    case "DAY_OF_WEEK_WEDNESDAY":
    case "WEDNESDAY":
      return 2;
    case "DAY_OF_WEEK_THURSDAY":
    case "THURSDAY":
      return 3;
    case "DAY_OF_WEEK_FRIDAY":
    case "FRIDAY":
      return 4;
    case "DAY_OF_WEEK_SATURDAY":
    case "SATURDAY":
      return 5;
    case "DAY_OF_WEEK_SUNDAY":
    case "SUNDAY":
      return 6;
    default:
      return -1;
  }
}

function hourFromLocalTime(s: string): number | null {
  const m = s.match(/^(\d{1,2})(?::(\d{2}))?$/);
  if (!m) return null;
  const h = Number(m[1]);
  if (h < 0 || h > 24) return null;
  return h;
}
