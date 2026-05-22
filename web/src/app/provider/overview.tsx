"use client";

import * as React from "react";
import Link from "next/link";
import { AuditEventCard } from "@/components/dashboard/audit-event-card";
import { PairedMachinesCard } from "@/components/dashboard/paired-machines-card";
import { StatsCard } from "@/components/dashboard/stats-card";
import {
  ProviderEmptyState,
  PROVIDER_EMPTY_OVERVIEW_SUBTITLE,
} from "@/components/dashboard/provider-empty-state";
import { browserApi } from "@/lib/api";
import { formatBytes, formatMoney } from "@/lib/format";
import { schedulerStateShortName } from "@/lib/proto-enum";
import { cn } from "@/lib/utils";
import type { ProviderDashboard } from "@/lib/types";

/**
 * Read a numeric field that may arrive on the wire under either its
 * canonical proto3-JSON camelCase name OR the stdlib `encoding/json`
 * snake_case name. gateway-bff serialises Connect-Go structs via
 * `encoding/json`, which honours the proto-gen struct tags
 * (`json:"cpu_percent,omitempty"`) and emits snake_case. Older
 * browser builds saw camelCase from a previous direct-Connect path.
 * Returns 0 when neither key is present (avoids `.toFixed()` on
 * `undefined` which crashes the entire panel — surfaced by the EPIC
 * #309 walk on 2026-05-21).
 */
function pickNum(
  obj: object,
  canonical: string,
  snake: string,
): number {
  const bag = obj as Record<string, unknown>;
  const v =
    (bag[canonical] as number | undefined) ??
    (bag[snake] as number | undefined) ??
    0;
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

export function ProvideOverview() {
  const [dash, setDash] = React.useState<ProviderDashboard | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [err, setErr] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    let timerId: ReturnType<typeof setInterval> | null = null;
    const load = async () => {
      try {
        const res = await browserApi().get<ProviderDashboard>(
          "/api/v1/provide/dashboard",
        );
        if (!cancelled) {
          setDash(res);
          // Stop polling once we know the caller owns zero providers —
          // the empty-state CTA is static, no point re-fetching every
          // 15s (#313). Polling resumes on a full page navigation
          // after the operator installs the daemon.
          if (res.has_provider === false && timerId !== null) {
            clearInterval(timerId);
            timerId = null;
          }
        }
      } catch (e) {
        if (!cancelled) setErr((e as Error).message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    timerId = setInterval(load, 15_000); // live-update every 15s
    return () => {
      cancelled = true;
      if (timerId !== null) clearInterval(timerId);
    };
  }, []);

  if (loading && !dash) {
    return (
      <div className="rounded-md border border-border p-8 text-center text-sm text-muted-foreground dark:border-border">
        Loading dashboard…
      </div>
    );
  }
  if (err && !dash) {
    return (
      <div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive dark:border-destructive/40 dark:bg-destructive/15 dark:text-destructive">
        Couldn&apos;t load the dashboard: {err}. Make sure the iogrid daemon is
        running and pointing at this account.
      </div>
    );
  }

  // Gate on has_provider BEFORE rendering the StatsCard grid — per #313
  // an operator with zero paired daemons must be pointed at /install,
  // never handed the em-dash skeleton (which falsely implies their
  // machine is up but idle). Backend contract: gateway-bff returns
  // {has_provider: false, providers: null, ...} for this case (#305).
  if (dash?.has_provider === false) {
    return <ProviderEmptyState subtitle={PROVIDER_EMPTY_OVERVIEW_SUBTITLE} />;
  }

  // gateway-bff serialises proto enums via Go's encoding/json, which
  // emits the numeric tag (e.g. `{"state": 1}`). Map back to the short
  // canonical name (`"ACTIVE"`) so the switch in StatusPill keeps working.
  // See #314.
  const state = schedulerStateShortName(dash?.state?.state) ?? "UNSPECIFIED";
  const usage = dash?.state?.usage;
  const reason = dash?.state?.reason;
  const earnings = dash?.earnings?.summary;
  const recent = dash?.recent_events ?? [];
  const providers = dash?.providers ?? [];

  return (
    <div className="space-y-6">
      {/*
       * "Paired machines" panel — #318. Shows display_name / status /
       * last-seen / registered for every daemon the caller owns. Hidden
       * when the BFF returns zero providers; the empty-state CTA on the
       * not-yet-paired path is handled by ProviderEmptyState (#313 /
       * PR #316) so we do NOT render anything here in that case to
       * avoid double empty-state UI.
       */}
      <PairedMachinesCard providers={providers} />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
        <StatsCard
          label="Scheduler"
          value={<StatusPill state={state} />}
          hint={reason || stateHint(state)}
        />
        <StatsCard
          label="Earnings this month"
          value={formatMoney(earnings?.totalEarned?.amount, earnings?.totalEarned?.currencyCode ?? "GRID")}
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
              ? `${(pickNum(usage, "cpuPercent", "cpu_percent")).toFixed(0)}% / ${(pickNum(usage, "memoryPercent", "memory_percent")).toFixed(0)}%`
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
            href="/provider/audit"
            className="text-sm text-muted-foreground hover:underline dark:text-muted-foreground"
          >
            Open transparency feed →
          </Link>
        </div>
        <ul className="mt-3 space-y-2">
          {recent.length === 0 ? (
            <li className="rounded-md border border-dashed border-border-strong p-4 text-center text-sm text-muted-foreground dark:border-border-strong">
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
        <QuickLink href="/provider/audit" label="Transparency feed" description="Live events" />
        <QuickLink href="/provider/schedule" label="Edit schedule" description="Caps + calendar + opt-ins" />
        <QuickLink href="/provider/earnings" label="Earnings + payouts" description="Daily / weekly / monthly" />
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
      className="rounded-md border border-border bg-card p-4 transition-colors hover:border-foreground/40 dark:border-border"
    >
      <p className="text-sm font-medium">{label}</p>
      <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
    </Link>
  );
}

function StatusPill({ state }: { state: string }) {
  const active = state === "ACTIVE";
  return (
    <span
      data-testid="provider-status"
      data-state={state}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold",
        active
          ? "bg-success/15 text-success dark:bg-success/15 dark:text-success"
          : "bg-warning/15 text-warning dark:bg-warning/15 dark:text-warning",
      )}
    >
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full",
          active ? "bg-success" : "bg-warning",
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
    <div className="rounded-md border border-border bg-card p-4 dark:border-border">
      <div className="flex items-baseline justify-between">
        <p className="text-sm font-medium">Bandwidth this month</p>
        <p className="text-xs text-muted-foreground">
          {formatBytes(used)} / {formatBytes(cap)}
        </p>
      </div>
      <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-muted dark:bg-muted">
        <div
          className={cn(
            "h-full transition-all",
            pct < 70 ? "bg-success" : pct < 90 ? "bg-warning" : "bg-destructive",
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
