"use client";

/**
 * BurnTimeseries — inline SVG bar chart of daily burns. Built without
 * Recharts to keep the public /burn page's bundle size small (the
 * page is anonymous-readable and should hit FCP fast).
 *
 * Visual style intentionally matches `EarningsChart` so the operator
 * recognises the chart family across the portal.
 */

import * as React from "react";
import type { BurnDailyPoint } from "@/lib/solana/burn";
import { formatToken } from "@/lib/solana/balances";

export interface BurnTimeseriesProps {
  data: BurnDailyPoint[];
  height?: number;
}

export function BurnTimeseries({ data, height = 220 }: BurnTimeseriesProps) {
  if (data.length === 0) {
    return (
      <div
        className="flex h-48 items-center justify-center rounded-md border border-dashed border-zinc-300 text-sm text-zinc-500 dark:border-zinc-700"
        data-testid="burn-timeseries-empty"
      >
        No burns recorded yet.
      </div>
    );
  }

  const w = 720;
  const h = height;
  const padding = { top: 12, right: 12, bottom: 32, left: 56 };
  const innerW = w - padding.left - padding.right;
  const innerH = h - padding.top - padding.bottom;

  const max = Math.max(...data.map((d) => d.burnedUi), 1);
  const barW = innerW / data.length;

  const ticks = 4;
  const yTicks = Array.from({ length: ticks + 1 }, (_, i) => (max / ticks) * i);
  const yFor = (v: number) => padding.top + innerH - (v / max) * innerH;
  const xFor = (i: number) => padding.left + i * barW;

  return (
    <figure
      className="w-full overflow-x-auto rounded-md border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900"
      aria-label="Daily $GRID burn time-series"
      data-testid="burn-timeseries"
    >
      <svg
        viewBox={`0 0 ${w} ${h}`}
        width="100%"
        height={h}
        role="img"
        className="text-zinc-600 dark:text-zinc-400"
      >
        {yTicks.map((t, i) => (
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
              {formatToken(t, 0)}
            </text>
          </g>
        ))}

        {data.map((d, i) => {
          const x = xFor(i) + barW * 0.15;
          const barWidth = barW * 0.7;
          const y = yFor(d.burnedUi);
          const barH = padding.top + innerH - y;
          return (
            <rect
              key={d.date}
              x={x}
              y={y}
              width={barWidth}
              height={Math.max(0, barH)}
              fill="rgb(244 63 94)"
              fillOpacity="0.85"
              data-testid={`burn-bar-${d.date}`}
            >
              <title>{`${d.date}: ${formatToken(d.burnedUi)} $GRID burned`}</title>
            </rect>
          );
        })}

        {[0, Math.floor(data.length / 2), data.length - 1]
          .filter((idx, i, arr) => arr.indexOf(idx) === i && idx < data.length)
          .map((idx) => (
            <text
              key={idx}
              x={xFor(idx) + barW / 2}
              y={h - 10}
              textAnchor="middle"
              fontSize="10"
              fill="currentColor"
            >
              {data[idx]?.date}
            </text>
          ))}
      </svg>
    </figure>
  );
}
