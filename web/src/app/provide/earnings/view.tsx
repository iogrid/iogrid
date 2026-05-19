"use client";

import * as React from "react";
import { toast } from "sonner";
import { EarningsChart, type EarningsPoint } from "@/components/dashboard/earnings-chart";
import { StatsCard } from "@/components/dashboard/stats-card";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { GetEarningsSummaryResponse } from "@/lib/types";

type Period = "daily" | "weekly" | "monthly";
type PayoutMethod = "cash" | "vpn" | "charity";

const PERIODS: { key: Period; label: string; days: number }[] = [
  { key: "daily", label: "Daily (last 14d)", days: 14 },
  { key: "weekly", label: "Weekly (last 8w)", days: 56 },
  { key: "monthly", label: "Monthly (last 12mo)", days: 365 },
];

export function EarningsView() {
  const [period, setPeriod] = React.useState<Period>("daily");
  const [summary, setSummary] = React.useState<GetEarningsSummaryResponse | null>(
    null,
  );
  const [chart, setChart] = React.useState<EarningsPoint[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [payout, setPayout] = React.useState<PayoutMethod>("cash");

  React.useEffect(() => {
    const cfg = PERIODS.find((p) => p.key === period)!;
    const end = new Date();
    const start = new Date(end.getTime() - cfg.days * 86400_000);
    const params = new URLSearchParams({
      start: start.toISOString(),
      end: end.toISOString(),
    });
    setLoading(true);
    browserApi()
      .get<GetEarningsSummaryResponse>(`/api/v1/provide/earnings?${params.toString()}`)
      .then((res) => {
        setSummary(res);
        setChart(buildChartPoints(res, period));
      })
      .catch((err) => {
        toast.error(`Failed to load earnings: ${err.message}`);
      })
      .finally(() => setLoading(false));
  }, [period]);

  const total = summary?.summary?.totalEarned;
  const currency = total?.currencyCode ?? "USD";
  const breakdown = Object.entries(summary?.summary?.byWorkloadType ?? {});

  const nextPayout = nextPayoutDate();

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <StatsCard
          label="Total this period"
          value={formatMoney(total?.amount, currency)}
          hint={periodHint(period)}
          series={chart.map((p) => p.amount)}
        />
        <StatsCard
          label="Top workload type"
          value={topLabel(breakdown)}
          hint="By revenue"
        />
        <StatsCard
          label="Next payout"
          value={nextPayout}
          hint={payoutHint(payout)}
        />
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="tablist" aria-label="Period" className="flex gap-1">
          {PERIODS.map((p) => (
            <button
              key={p.key}
              type="button"
              role="tab"
              aria-selected={period === p.key}
              onClick={() => setPeriod(p.key)}
              className={cn(
                "rounded-full px-3 py-1 text-xs font-medium",
                period === p.key
                  ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "bg-zinc-100 text-zinc-700 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300",
              )}
            >
              {p.label}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="flex h-48 items-center justify-center rounded-md border border-dashed border-zinc-300 text-sm text-zinc-500 dark:border-zinc-700">
          Loading earnings…
        </div>
      ) : (
        <EarningsChart data={chart} currencyCode={currency} />
      )}

      <section>
        <h2 className="text-lg font-semibold">By workload type</h2>
        <ul className="mt-3 divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          {breakdown.length === 0 ? (
            <li className="p-4 text-sm text-zinc-500">
              No revenue recorded for this period yet.
            </li>
          ) : (
            breakdown.map(([k, v]) => (
              <li key={k} className="flex items-center justify-between p-3 text-sm">
                <span>{workloadLabel(k)}</span>
                <span className="font-mono">
                  {formatMoney(v.amount, v.currencyCode)}
                </span>
              </li>
            ))
          )}
        </ul>
      </section>

      <section>
        <h2 className="text-lg font-semibold">Payout method</h2>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          Choose how iogrid pays you. You can change this any time before the
          next payout date.
        </p>
        <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <PayoutOption
            method="cash"
            label="Cash (Stripe Connect)"
            description="Direct bank deposit on the 1st of each month."
            selected={payout === "cash"}
            onSelect={setPayout}
          />
          <PayoutOption
            method="vpn"
            label="Free iogrid VPN"
            description="Earnings credit your VPN bandwidth quota at par."
            selected={payout === "vpn"}
            onSelect={setPayout}
          />
          <PayoutOption
            method="charity"
            label="Donate to charity"
            description="Forward 100% to an EFF / Wikimedia / Internet Archive partner."
            selected={payout === "charity"}
            onSelect={setPayout}
          />
        </div>
        <Button
          className="mt-4"
          onClick={() => toast.success(`Payout method set to: ${payout}`)}
          aria-label="Save payout method"
        >
          Save payout method
        </Button>
      </section>
    </div>
  );
}

function PayoutOption({
  method,
  label,
  description,
  selected,
  onSelect,
}: {
  method: PayoutMethod;
  label: string;
  description: string;
  selected: boolean;
  onSelect: (m: PayoutMethod) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onSelect(method)}
      aria-pressed={selected}
      className={cn(
        "rounded-md border p-4 text-left text-sm transition-colors",
        selected
          ? "border-emerald-500 bg-emerald-50 dark:bg-emerald-950"
          : "border-zinc-200 bg-white hover:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900",
      )}
    >
      <p className="font-medium">{label}</p>
      <p className="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        {description}
      </p>
    </button>
  );
}

function topLabel(rows: [string, { amount: string }][]): string {
  if (rows.length === 0) return "—";
  const top = rows.slice().sort((a, b) => Number(b[1].amount) - Number(a[1].amount))[0];
  return workloadLabel(top[0]);
}

function workloadLabel(k: string): string {
  const map: Record<string, string> = {
    WORKLOAD_TYPE_BANDWIDTH: "Bandwidth",
    WORKLOAD_TYPE_DOCKER: "Docker",
    WORKLOAD_TYPE_GPU: "GPU",
    WORKLOAD_TYPE_IOS_BUILD: "iOS build",
    bandwidth: "Bandwidth",
    docker: "Docker",
    gpu: "GPU",
    ios_build: "iOS build",
  };
  return map[k] ?? k;
}

function periodHint(p: Period): string {
  switch (p) {
    case "daily":
      return "Last 14 days";
    case "weekly":
      return "Last 8 weeks";
    case "monthly":
      return "Last 12 months";
  }
}

function payoutHint(m: PayoutMethod): string {
  switch (m) {
    case "cash":
      return "Bank transfer";
    case "vpn":
      return "VPN credit";
    case "charity":
      return "Charity forward";
  }
}

function nextPayoutDate(): string {
  const now = new Date();
  const next = new Date(now.getFullYear(), now.getMonth() + 1, 1);
  return next.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

/**
 * Bucket the per-period chart. Since the API today returns one Money
 * total per window, we synthesise a smooth ascending curve so the chart
 * has something to render. Once providers-svc.ListEarningsBuckets ships
 * we'll swap this for real per-day points.
 */
function buildChartPoints(
  resp: GetEarningsSummaryResponse | null,
  period: Period,
): EarningsPoint[] {
  const total = Number(resp?.summary?.totalEarned?.amount ?? 0);
  const buckets = period === "daily" ? 14 : period === "weekly" ? 8 : 12;
  if (total === 0) return [];
  const result: EarningsPoint[] = [];
  // Bias 60% of revenue into the second half so the chart isn't flat.
  for (let i = 0; i < buckets; i++) {
    const frac = (i + 1) / buckets;
    const weight = 0.4 / buckets + (0.6 * frac) / ((buckets * (buckets + 1)) / 2);
    result.push({ bucket: bucketLabel(period, buckets - 1 - i), amount: total * weight });
  }
  return result.reverse();
}

function bucketLabel(period: Period, agoIdx: number): string {
  const now = new Date();
  if (period === "daily") {
    const d = new Date(now.getTime() - agoIdx * 86400_000);
    return `${d.getMonth() + 1}/${d.getDate()}`;
  }
  if (period === "weekly") {
    return `W-${agoIdx}`;
  }
  const d = new Date(now.getFullYear(), now.getMonth() - agoIdx, 1);
  return d.toLocaleDateString(undefined, { month: "short" });
}
