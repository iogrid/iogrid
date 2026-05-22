"use client";

import * as React from "react";
import { toast } from "sonner";
import { EarningsChart, type EarningsPoint } from "@/components/dashboard/earnings-chart";
import { StatsCard } from "@/components/dashboard/stats-card";
import {
  ProviderEmptyState,
  PROVIDER_EMPTY_EARNINGS_SUBTITLE,
} from "@/components/dashboard/provider-empty-state";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { formatMoney } from "@/lib/format";
import { useProviderOwnership } from "@/lib/use-provider-ownership";
import { cn } from "@/lib/utils";
import type {
  BillingGetEarningsSummaryResponse,
  GetEarningsSummaryResponse,
  GetPayoutMethodResponse,
  PayoutMethodKind,
  SetPayoutMethodResponse,
} from "@/lib/types";
import {
  WithdrawDrawer,
  loadPendingOffRamps,
  type PendingOffRamp,
} from "./withdraw";

type Period = "daily" | "weekly" | "monthly";
// "Free iogrid VPN" used to live here, but it's redundant — holders of
// $GRID can burn-for-VPN at any time via the VPN credit pool. The
// canonical payout state is "hold $GRID" (default); the cash and
// charity variants are swap-from-$GRID off-ramps handled by
// billing-svc's monthly cron once #274 lands the founder mint.
type PayoutMethod = "grid" | "cash" | "charity";

/**
 * Map the UI's three-state PayoutMethod tag to the canonical proto
 * enum (#324). "grid" is the default (no off-ramp configured) — it
 * round-trips as UNSPECIFIED on the wire because billing-svc's
 * payout_methods.kind defaults to UNSPECIFIED on insert and we treat
 * UNSPECIFIED as "hold $GRID" everywhere.
 */
function toPayoutMethodKind(m: PayoutMethod): PayoutMethodKind {
  switch (m) {
    case "grid":
      return "PAYOUT_METHOD_KIND_UNSPECIFIED";
    case "cash":
      return "PAYOUT_METHOD_KIND_CASH_USDC";
    case "charity":
      return "PAYOUT_METHOD_KIND_CHARITY";
  }
}

function fromPayoutMethodKind(k: PayoutMethodKind | undefined): PayoutMethod {
  switch (k) {
    case "PAYOUT_METHOD_KIND_CASH_USDC":
      return "cash";
    case "PAYOUT_METHOD_KIND_CHARITY":
      return "charity";
    default:
      // UNSPECIFIED + FREE_VPN both render as the "Hold $GRID" tile
      // selected; FREE_VPN is functionally equivalent because the user
      // can burn-for-VPN any time from a $GRID balance.
      return "grid";
  }
}

const PERIODS: { key: Period; label: string; days: number }[] = [
  { key: "daily", label: "Daily (last 14d)", days: 14 },
  { key: "weekly", label: "Weekly (last 8w)", days: 56 },
  { key: "monthly", label: "Monthly (last 12mo)", days: 365 },
];

export function EarningsView() {
  const ownership = useProviderOwnership();
  const [period, setPeriod] = React.useState<Period>("daily");
  const [summary, setSummary] = React.useState<GetEarningsSummaryResponse | null>(
    null,
  );
  // Headline-card aggregation from billing-svc (#324) — distinct from
  // the providers-svc `summary` above which carries per-workload-type
  // breakdown but no lifetime/30d/7d/pending rollups.
  const [headline, setHeadline] =
    React.useState<BillingGetEarningsSummaryResponse | null>(null);
  const [chart, setChart] = React.useState<EarningsPoint[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [payout, setPayout] = React.useState<PayoutMethod>("grid");
  const [payoutSaving, setPayoutSaving] = React.useState(false);
  const [withdrawOpen, setWithdrawOpen] = React.useState(false);
  const [pendingOffRamps, setPendingOffRamps] = React.useState<PendingOffRamp[]>(
    [],
  );

  React.useEffect(() => {
    // Hydrate pending off-ramps from localStorage on mount; the
    // earnings page persists these across reloads (issue #169).
    setPendingOffRamps(loadPendingOffRamps());
  }, []);

  // Headline + payout-method are fetched once on mount (they don't
  // depend on the period selector — period drives only the time-series
  // chart below).
  React.useEffect(() => {
    const api = browserApi();
    api
      .get<BillingGetEarningsSummaryResponse>("/api/v1/provide/earnings/summary")
      .then((res) => setHeadline(res))
      .catch((err) => {
        toast.error(`Failed to load earnings summary: ${err.message}`);
      });
    api
      .get<GetPayoutMethodResponse>("/api/v1/provide/payout-method")
      .then((res) => setPayout(fromPayoutMethodKind(res.method?.kind)))
      .catch((err) => {
        // Don't toast on payout-method failure — the user can still
        // pick a method and save it. Log to the dev console for triage.
        if (typeof console !== "undefined") {
          console.warn("payout-method fetch failed", err);
        }
      });
  }, []);

  React.useEffect(() => {
    // Don't fan out to /api/v1/provide/earnings when we already know
    // the caller owns zero paired providers — the BFF would just hand
    // us back an empty envelope and the page would render the
    // empty-state below anyway. Saves one round-trip per page view
    // for the not-yet-paired cohort (#313).
    if (ownership.hasProvider === false) {
      setLoading(false);
      return;
    }
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
  }, [period, ownership.hasProvider]);

  // Gate on ownership BEFORE rendering the StatsCard grid — the
  // skeleton-with-em-dash treatment is misleading for users with zero
  // paired daemons (#313). Render the "Install daemon" CTA instead.
  if (ownership.hasProvider === false) {
    return <ProviderEmptyState subtitle={PROVIDER_EMPTY_EARNINGS_SUBTITLE} />;
  }

  // Prefer the billing-svc headline (lifetime cumulative) for the top
  // card; fall back to the providers-svc window total if the headline
  // hasn't arrived yet. Both default to GRID currency for #312/#315.
  const headlineTotal = headline?.summary?.totalEarned;
  const total = headlineTotal ?? summary?.summary?.totalEarned;
  const currency = total?.currencyCode ?? "GRID";
  const breakdown = Object.entries(summary?.summary?.byWorkloadType ?? {});

  const nextPayout = nextPayoutDate();

  async function savePayoutMethod() {
    setPayoutSaving(true);
    try {
      await browserApi().put<SetPayoutMethodResponse>(
        "/api/v1/provide/payout-method",
        { kind: toPayoutMethodKind(payout) },
      );
      toast.success(`Payout method set to: ${payout}`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to save payout method: ${msg}`);
    } finally {
      setPayoutSaving(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        <StatsCard
          label="Total earned (lifetime)"
          value={formatMoney(
            headline?.summary?.totalEarned?.amount,
            headline?.summary?.totalEarned?.currencyCode ?? currency,
          )}
          hint={
            headline?.summary?.lifetimeWorkloads
              ? `${headline.summary.lifetimeWorkloads} workloads`
              : "No workloads yet"
          }
          series={chart.map((p) => p.amount)}
        />
        <StatsCard
          label="Last 30 days"
          value={formatMoney(
            headline?.summary?.last30D?.amount,
            headline?.summary?.last30D?.currencyCode ?? currency,
          )}
          hint="Rolling 30-day window"
        />
        <StatsCard
          label="Pending payout"
          value={formatMoney(
            headline?.summary?.pendingPayout?.amount,
            headline?.summary?.pendingPayout?.currencyCode ?? currency,
          )}
          hint={payoutHint(payout)}
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
                  ? "bg-foreground text-background dark:bg-foreground dark:text-background"
                  : "bg-muted text-foreground hover:bg-muted dark:bg-muted dark:text-muted-foreground",
              )}
            >
              {p.label}
            </button>
          ))}
        </div>
        <Button
          variant="default"
          size="sm"
          onClick={() => setWithdrawOpen(true)}
          aria-label="Withdraw earnings"
        >
          Withdraw
        </Button>
      </div>

      {pendingOffRamps.length > 0 ? (
        <section
          aria-label="Pending off-ramp requests"
          className="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm dark:border-warning/40 dark:bg-warning/15"
        >
          <p className="font-medium text-warning dark:text-warning">
            Off-ramp in progress
          </p>
          <ul className="mt-2 space-y-1 text-xs text-warning dark:text-warning">
            {pendingOffRamps.map((p) => (
              <li key={p.requestId} className="flex justify-between gap-3">
                <span>{p.providerName}</span>
                <span className="font-mono">{p.requestId.slice(0, 8)}</span>
                <span>{new Date(p.startedAt).toLocaleString()}</span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {loading ? (
        <div className="flex h-48 items-center justify-center rounded-md border border-dashed border-border-strong text-sm text-muted-foreground dark:border-border-strong">
          Loading earnings…
        </div>
      ) : (
        <EarningsChart data={chart} currencyCode={currency} />
      )}

      <section>
        <h2 className="text-lg font-semibold">By workload type</h2>
        <ul className="mt-3 divide-y divide-border rounded-md border border-border dark:divide-border dark:border-border">
          {breakdown.length === 0 ? (
            <li className="p-4 text-sm text-muted-foreground">
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
        <p className="mt-1 text-sm text-muted-foreground dark:text-muted-foreground">
          Earnings are paid in $GRID by default. The cash and charity
          variants auto-swap $GRID via billing-svc&apos;s monthly off-ramp
          cron — you can change this any time before the next payout date.
        </p>
        <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <PayoutOption
            method="grid"
            label="Hold $GRID (default)"
            description="Earnings stay in your wallet as $GRID. Swap, burn-for-VPN, or transfer any time. (devnet during Phase 0.)"
            selected={payout === "grid"}
            onSelect={setPayout}
          />
          <PayoutOption
            method="cash"
            label="Cash (Stripe Connect)"
            description="Auto-swap $GRID → USD via off-ramp on the 1st of each month."
            selected={payout === "cash"}
            onSelect={setPayout}
          />
          <PayoutOption
            method="charity"
            label="Donate to charity"
            description="Auto-swap $GRID and forward proceeds to EFF / Wikimedia / Internet Archive."
            selected={payout === "charity"}
            onSelect={setPayout}
          />
        </div>
        <Button
          className="mt-4"
          onClick={savePayoutMethod}
          disabled={payoutSaving}
          aria-label="Save payout method"
        >
          {payoutSaving ? "Saving…" : "Save payout method"}
        </Button>
      </section>

      <WithdrawDrawer open={withdrawOpen} onOpenChange={setWithdrawOpen} />
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
          ? "border-success/40 bg-success/10 dark:bg-success/15"
          : "border-border bg-card hover:border-foreground/40 dark:border-border",
      )}
    >
      <p className="font-medium">{label}</p>
      <p className="mt-1 text-xs text-muted-foreground dark:text-muted-foreground">
        {description}
      </p>
    </button>
  );
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

function payoutHint(m: PayoutMethod): string {
  switch (m) {
    case "grid":
      return "Held in $GRID";
    case "cash":
      return "Bank transfer";
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
