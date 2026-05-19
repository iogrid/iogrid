import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * StatsCard — the bordered tile used everywhere a single number needs a
 * label and (optionally) a tiny sparkline. Server-component compatible.
 *
 * Sparkline uses an inline SVG so we don't drag a chart library into
 * the dashboard above-the-fold. It accepts up to ~30 values and
 * normalises them to fit the 80×24 frame.
 */
export interface StatsCardProps {
  label: string;
  value: React.ReactNode;
  /** Sub-label shown under the big number (e.g. "this month"). */
  hint?: string;
  /** Delta vs. previous period — colour-coded. */
  delta?: { value: string; direction: "up" | "down" | "flat" };
  /** Numeric series for the inline sparkline. */
  series?: number[];
  className?: string;
}

export function StatsCard({
  label,
  value,
  hint,
  delta,
  series,
  className,
}: StatsCardProps) {
  return (
    <div
      className={cn(
        "rounded-lg border border-zinc-200 bg-white p-5 shadow-sm dark:border-zinc-800 dark:bg-zinc-900",
        className,
      )}
    >
      <div className="flex items-start justify-between">
        <h3 className="text-sm font-medium text-zinc-500 dark:text-zinc-400">
          {label}
        </h3>
        {delta ? <DeltaPill {...delta} /> : null}
      </div>
      <p className="mt-2 text-3xl font-semibold tracking-tight">{value}</p>
      <div className="mt-3 flex items-end justify-between gap-2">
        {hint ? (
          <p className="text-xs text-zinc-500 dark:text-zinc-400">{hint}</p>
        ) : (
          <span />
        )}
        {series && series.length > 1 ? <Sparkline values={series} /> : null}
      </div>
    </div>
  );
}

function DeltaPill({
  value,
  direction,
}: {
  value: string;
  direction: "up" | "down" | "flat";
}) {
  const color =
    direction === "up"
      ? "bg-emerald-50 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
      : direction === "down"
        ? "bg-rose-50 text-rose-700 dark:bg-rose-950 dark:text-rose-300"
        : "bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300";
  const arrow = direction === "up" ? "↑" : direction === "down" ? "↓" : "→";
  return (
    <span
      className={cn(
        "rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
        color,
      )}
    >
      {arrow} {value}
    </span>
  );
}

function Sparkline({ values }: { values: number[] }) {
  const w = 80;
  const h = 24;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const stepX = w / (values.length - 1);
  const points = values
    .map((v, i) => {
      const x = (i * stepX).toFixed(1);
      const y = (h - ((v - min) / span) * h).toFixed(1);
      return `${x},${y}`;
    })
    .join(" ");
  return (
    <svg
      width={w}
      height={h}
      viewBox={`0 0 ${w} ${h}`}
      role="img"
      aria-label="trend"
      className="text-zinc-400 dark:text-zinc-500"
    >
      <polyline
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        points={points}
      />
    </svg>
  );
}
