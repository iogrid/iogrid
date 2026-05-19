"use client";

import * as React from "react";
import Link from "next/link";
import { AuditEventCard } from "@/components/dashboard/audit-event-card";
import { StatsCard } from "@/components/dashboard/stats-card";
import { browserApi } from "@/lib/api";
import { formatBytes, formatMoney } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { ProviderDashboard, SchedulerState } from "@/lib/types";

export function ProvideOverview() {
  const [dash, setDash] = React.useState<ProviderDashboard | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [err, setErr] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await browserApi().get<ProviderDashboard>(
          "/api/v1/provide/dashboard",
        );
        if (!cancelled) setDash(res);
      } catch (e) {
        if (!cancelled) setErr((e as Error).message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    const id = setInterval(load, 15_000); // live-update every 15s
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  if (loading && !dash) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading dashboard…
      </div>
    );
  }
  if (err && !dash) {
    return (
      <div className="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950 dark:text-rose-300">
        Couldn&apos;t load the dashboard: {err}. Make sure the iogrid daemon is
        running and pointing at this account.
      </div>
    );
  }

  const state = dash?.state?.state ?? "UNSPECIFIED";
  const usage = dash?.state?.usage;
  const reason = dash?.state?.reason;
  const earnings = dash?.earnings?.summary;
  const recent = dash?.recent_events ?? [];

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
        <StatsCard
          label="Scheduler"
          value={<StatusPill state={state} />}
          hint={reason || stateHint(state)}
        />
        <StatsCard
          label="Earnings this month"
          value={formatMoney(earnings?.totalEarned?.amount, earnings?.totalEarned?.currencyCode ?? "USD")}
          hint="So far this period"
        />
        <StatsCard
          label="Bandwidth used"
          value={formatBytes(usage?.bandwidthUsedBytesThisMonth ?? "0")}
          hint="Against current cap"
        />
        <StatsCard
          label="CPU / Memory"
          value={
            usage
              ? `${usage.cpuPercent.toFixed(0)}% / ${usage.memoryPercent.toFixed(0)}%`
              : "—"
          }
          hint="Current load"
        />
      </div>

      <BandwidthProgressBar
        usedBytes={usage?.bandwidthUsedBytesThisMonth ?? "0"}
      />

      <section>
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold">Recent activity</h2>
          <Link
            href="/provide/audit"
            className="text-sm text-zinc-600 hover:underline dark:text-zinc-400"
          >
            Open transparency feed →
          </Link>
        </div>
        <ul className="mt-3 space-y-2">
          {recent.length === 0 ? (
            <li className="rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700">
              No audit events recorded yet. Once your daemon connects and
              starts accepting workloads, the live feed will populate here.
            </li>
          ) : (
            recent.slice(0, 5).map((ev, i) => (
              <li key={ev.id?.value ?? i}>
                <AuditEventCard event={ev} />
              </li>
            ))
          )}
        </ul>
      </section>

      <section className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <QuickLink href="/provide/audit" label="Transparency feed" description="Live events" />
        <QuickLink href="/provide/schedule" label="Edit schedule" description="Caps + calendar + opt-ins" />
        <QuickLink href="/provide/earnings" label="Earnings + payouts" description="Daily / weekly / monthly" />
      </section>
    </div>
  );
}

function QuickLink({
  href,
  label,
  description,
}: {
  href: string;
  label: string;
  description: string;
}) {
  return (
    <Link
      href={href}
      className="rounded-md border border-zinc-200 bg-white p-4 transition-colors hover:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900"
    >
      <p className="text-sm font-medium">{label}</p>
      <p className="mt-0.5 text-xs text-zinc-500">{description}</p>
    </Link>
  );
}

function StatusPill({ state }: { state: SchedulerState | string }) {
  const active = state === "ACTIVE";
  return (
    <span
      data-testid="provider-status"
      data-state={state}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold",
        active
          ? "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300"
          : "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
      )}
    >
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full",
          active ? "bg-emerald-500" : "bg-amber-500",
        )}
        aria-hidden
      />
      {humanState(state)}
    </span>
  );
}

function humanState(s: string): string {
  switch (s) {
    case "ACTIVE":
      return "Active";
    case "PAUSED_BANDWIDTH_CAP":
      return "Paused — bandwidth cap";
    case "PAUSED_CPU_CAP":
      return "Paused — CPU cap";
    case "PAUSED_MEMORY_CAP":
      return "Paused — memory cap";
    case "PAUSED_OUTSIDE_CALENDAR":
      return "Paused — outside calendar";
    case "PAUSED_USER_ACTIVE":
      return "Paused — user active";
    case "PAUSED_OPERATIONS":
      return "Paused — ops";
    default:
      return "Unknown";
  }
}

function stateHint(s: string): string {
  if (s === "ACTIVE") return "Accepting workloads";
  return "Will resume automatically when conditions clear";
}

function BandwidthProgressBar({ usedBytes }: { usedBytes: string }) {
  const used = Number(usedBytes);
  const cap = 50 * 1024 ** 3; // default 50 GB until real cap is fetched
  const pct = Math.min(100, (used / cap) * 100);
  return (
    <div className="rounded-md border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="flex items-baseline justify-between">
        <p className="text-sm font-medium">Bandwidth this month</p>
        <p className="text-xs text-zinc-500">
          {formatBytes(used)} / {formatBytes(cap)}
        </p>
      </div>
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-zinc-100 dark:bg-zinc-800">
        <div
          className={cn(
            "h-full transition-all",
            pct < 70 ? "bg-emerald-500" : pct < 90 ? "bg-amber-500" : "bg-rose-500",
          )}
          style={{ width: `${pct}%` }}
          aria-valuenow={Math.round(pct)}
          aria-valuemin={0}
          aria-valuemax={100}
          role="progressbar"
        />
      </div>
    </div>
  );
}
