"use client";

import * as React from "react";
import { formatMoney } from "@/lib/format";

/**
 * EarningsChart — small SVG-only time-series renderer so we can ship
 * without a chart dep. We avoid Recharts/Chart.js because either pulls
 * ~80kB into the first paint, and we only need one curve + axis labels.
 *
 * Data is a list of `{ bucket, amount }` rows. The component picks a
 * reasonable y-tick interval and draws an area + line. Hovering a point
 * highlights its bucket.
 */
export interface EarningsPoint {
  bucket: string;
  amount: number;
}

export interface EarningsChartProps {
  data: EarningsPoint[];
  currencyCode?: string;
  height?: number;
}

export function EarningsChart({
  data,
  currencyCode = "USD",
  height = 200,
}: EarningsChartProps) {
  // Hooks declared at top to satisfy rules-of-hooks; we then early-return.
  const [hover, setHover] = React.useState<number | null>(null);

  if (data.length === 0) {
    return (
      <div className="flex h-48 items-center justify-center rounded-md border border-dashed border-zinc-300 text-sm text-zinc-500 dark:border-zinc-700">
        No earnings recorded yet.
      </div>
    );
  }

  const w = 720;
  const h = height;
  const padding = { top: 12, right: 12, bottom: 28, left: 48 };
  const innerW = w - padding.left - padding.right;
  const innerH = h - padding.top - padding.bottom;

  const max = Math.max(...data.map((d) => d.amount), 1);
  const stepX = innerW / Math.max(1, data.length - 1);
  const yFor = (v: number) =>
    padding.top + innerH - (v / max) * innerH;
  const xFor = (i: number) => padding.left + i * stepX;

  const linePts = data.map((d, i) => `${xFor(i)},${yFor(d.amount)}`).join(" ");
  const areaPath =
    `M ${xFor(0)} ${padding.top + innerH} ` +
    data.map((d, i) => `L ${xFor(i)} ${yFor(d.amount)}`).join(" ") +
    ` L ${xFor(data.length - 1)} ${padding.top + innerH} Z`;

  // Tick interval: aim for ~4 ticks.
  const tickCount = 4;
  const ticks = Array.from({ length: tickCount + 1 }, (_, i) =>
    (max / tickCount) * i,
  );

  return (
    <figure
      className="w-full overflow-x-auto rounded-md border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900"
      aria-label="Earnings time-series"
    >
      <svg
        viewBox={`0 0 ${w} ${h}`}
        width="100%"
        height={h}
        role="img"
        onMouseLeave={() => setHover(null)}
        className="text-zinc-600 dark:text-zinc-400"
      >
        {/* y-axis grid */}
        {ticks.map((t, i) => (
          <g key={i}>
            <line
              x1={padding.left}
              x2={w - padding.right}
              y1={yFor(t)}
              y2={yFor(t)}
              stroke="currentColor"
              strokeOpacity="0.1"
            />
            <text
              x={padding.left - 6}
              y={yFor(t)}
              textAnchor="end"
              dominantBaseline="middle"
              fontSize="10"
              fill="currentColor"
            >
              {formatMoney(t, currencyCode)}
            </text>
          </g>
        ))}
        {/* x-axis labels — show first, middle, last */}
        {[0, Math.floor(data.length / 2), data.length - 1]
          .filter((idx, i, arr) => arr.indexOf(idx) === i)
          .map((idx) => (
            <text
              key={idx}
              x={xFor(idx)}
              y={h - 8}
              textAnchor="middle"
              fontSize="10"
              fill="currentColor"
            >
              {data[idx].bucket}
            </text>
          ))}
        {/* area + line */}
        <path d={areaPath} fill="currentColor" fillOpacity="0.08" />
        <polyline
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          points={linePts}
        />
        {/* hover hit areas */}
        {data.map((d, i) => (
          <g key={i}>
            <rect
              x={xFor(i) - stepX / 2}
              y={padding.top}
              width={stepX}
              height={innerH}
              fill="transparent"
              onMouseEnter={() => setHover(i)}
            />
            <circle
              cx={xFor(i)}
              cy={yFor(d.amount)}
              r={hover === i ? 4 : 2}
              fill="currentColor"
            />
          </g>
        ))}
      </svg>
      {hover !== null ? (
        <figcaption
          aria-live="polite"
          className="mt-1 text-xs text-zinc-600 dark:text-zinc-400"
        >
          {data[hover].bucket}:{" "}
          <strong className="font-semibold">
            {formatMoney(data[hover].amount, currencyCode)}
          </strong>
        </figcaption>
      ) : (
        <figcaption className="mt-1 text-xs text-zinc-500">
          Hover a point to see the bucket value.
        </figcaption>
      )}
    </figure>
  );
}
