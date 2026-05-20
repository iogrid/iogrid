"use client";

import * as React from "react";
import { toast } from "sonner";
import { useSSE } from "@/lib/sse";
import { browserApi } from "@/lib/api";
import { AuditEventCard } from "@/components/dashboard/audit-event-card";
import {
  ProviderEmptyState,
  PROVIDER_EMPTY_AUDIT_SUBTITLE,
} from "@/components/dashboard/provider-empty-state";
import { Button } from "@/components/ui/button";
import { useProviderOwnership } from "@/lib/use-provider-ownership";
import { cn } from "@/lib/utils";
import type { AuditEvent, SchedulingConfig } from "@/lib/types";

/** Range chip choices. Filtering happens client-side for simplicity. */
const FILTERS = [
  { key: "all", label: "All" },
  { key: "active", label: "Active" },
  { key: "24h", label: "Last 24h" },
  { key: "7d", label: "Last 7d" },
] as const;
type FilterKey = (typeof FILTERS)[number]["key"];

type FeedEvent = AuditEvent & { occurredAtMs: number };

const SSE_URL =
  process.env.NEXT_PUBLIC_GATEWAY_URL
    ? `${process.env.NEXT_PUBLIC_GATEWAY_URL.replace(/\/$/, "")}/api/v1/provide/audit/stream`
    : "/api/v1/provide/audit/stream";

export function AuditFeed() {
  const ownership = useProviderOwnership();
  const [paused, setPaused] = React.useState(false);
  const [filter, setFilter] = React.useState<FilterKey>("all");
  const [nowMs, setNowMs] = React.useState(Date.now());

  // Tick the relative timestamps once a second.
  React.useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // Don't open the SSE EventSource until we've confirmed the caller
  // owns at least one provider. Without this, /provide/audit/stream
  // would 404 in a tight reconnect loop for the not-yet-paired cohort
  // (gateway-bff returns 404 with code=no_provider in that case).
  // We pass `paused` to useSSE so it never spawns the EventSource
  // until ownership resolves true (#313).
  const sseSuppressed = ownership.hasProvider !== true;
  const { events: liveEvents, status, clear } = useSSE<FeedEvent>({
    url: SSE_URL,
    paused: paused || sseSuppressed,
    parse: (raw) => {
      try {
        const parsed = JSON.parse(raw) as AuditEvent;
        return {
          ...parsed,
          occurredAtMs: parsed.occurredAt
            ? Date.parse(parsed.occurredAt)
            : Date.now(),
        };
      } catch {
        return null;
      }
    },
  });

  const filtered = React.useMemo(() => {
    const cut = filterCutoff(filter, nowMs);
    return liveEvents
      .filter((e) => e.occurredAtMs >= cut)
      .slice()
      .reverse(); // newest first
  }, [liveEvents, filter, nowMs]);

  const blockMutation = React.useCallback(
    async (kind: "category" | "customer" | "destination", value: string) => {
      try {
        const api = browserApi();
        const current = await api.get<{ config: SchedulingConfig }>(
          "/api/v1/provide/schedule",
        );
        const cfg = current.config ?? {};
        const next: SchedulingConfig = { ...cfg };
        if (kind === "category") {
          const dis = new Set(next.categoryOptIn?.disallowedCategories ?? []);
          dis.add(value);
          const allowed = (next.categoryOptIn?.allowedCategories ?? []).filter(
            (c) => c !== value,
          );
          next.categoryOptIn = {
            allowedCategories: allowed,
            disallowedCategories: Array.from(dis),
          };
        }
        if (kind === "destination") {
          const blk = new Set(next.destinationPolicy?.destinationBlocklist ?? []);
          blk.add(value);
          next.destinationPolicy = {
            destinationBlocklist: Array.from(blk),
            perCustomerMinutesCap:
              next.destinationPolicy?.perCustomerMinutesCap ?? 0,
          };
        }
        if (kind === "customer") {
          // Customer-level blocks are stored as a metadata blocklist on
          // destination_policy with a `customer:` prefix; the daemon
          // enforces this client-side until a dedicated proto field
          // exists.
          const blk = new Set(next.destinationPolicy?.destinationBlocklist ?? []);
          blk.add(`customer:${value}`);
          next.destinationPolicy = {
            destinationBlocklist: Array.from(blk),
            perCustomerMinutesCap:
              next.destinationPolicy?.perCustomerMinutesCap ?? 0,
          };
        }
        await api.post("/api/v1/provide/schedule", { config: next });
        toast.success(`Blocked ${kind}: ${value}`);
      } catch (err) {
        toast.error(`Failed to block ${kind}: ${(err as Error).message}`);
      }
    },
    [],
  );

  // Gate on ownership BEFORE rendering the SSE controls (#313). The
  // transparency feed is meaningless without a paired daemon producing
  // events; show the "Install daemon" CTA so the user knows what to do.
  if (ownership.hasProvider === false) {
    return <ProviderEmptyState subtitle={PROVIDER_EMPTY_AUDIT_SUBTITLE} />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="tablist" aria-label="Filter" className="flex gap-1">
          {FILTERS.map((f) => (
            <button
              key={f.key}
              type="button"
              role="tab"
              aria-selected={filter === f.key}
              onClick={() => setFilter(f.key)}
              className={cn(
                "rounded-full px-3 py-1 text-xs font-medium transition-colors",
                filter === f.key
                  ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
                  : "bg-zinc-100 text-zinc-700 hover:bg-zinc-200 dark:bg-zinc-800 dark:text-zinc-300 dark:hover:bg-zinc-700",
              )}
            >
              {f.label}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <StatusPill status={status} />
          <Button
            size="sm"
            variant="outline"
            onClick={() => setPaused((p) => !p)}
            aria-pressed={paused}
          >
            {paused ? "Resume stream" : "Pause stream"}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => clear()}
            aria-label="Clear feed"
          >
            Clear
          </Button>
          <a
            href={exportCsvUrl(filtered)}
            download={`iogrid-audit-${new Date().toISOString().slice(0, 10)}.csv`}
            className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm font-medium hover:bg-zinc-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            Export CSV
          </a>
        </div>
      </div>

      <ul
        aria-live="polite"
        aria-label="Audit events"
        data-testid="audit-list"
        className="space-y-2"
      >
        {filtered.length === 0 ? (
          <li className="rounded-md border border-dashed border-zinc-300 p-6 text-center text-sm text-zinc-500 dark:border-zinc-700">
            {status === "open"
              ? "Listening for events… your machine is connected but hasn't received any workloads in this window yet."
              : "Connecting to the transparency stream…"}
          </li>
        ) : (
          filtered.map((ev, idx) => (
            <li key={ev.id?.value ?? `${ev.occurredAtMs}-${idx}`}>
              <AuditEventCard
                event={ev}
                nowMs={nowMs}
                onBlockCategory={(c) => blockMutation("category", c)}
                onBlockCustomer={(c) => blockMutation("customer", c)}
                onBlockDestination={(c) => blockMutation("destination", c)}
              />
            </li>
          ))
        )}
      </ul>
    </div>
  );
}

function StatusPill({
  status,
}: {
  status: "connecting" | "open" | "closed" | "error" | "unavailable";
}) {
  const map: Record<string, { dot: string; label: string }> = {
    open: { dot: "bg-emerald-500", label: "Live" },
    connecting: { dot: "bg-amber-500 animate-pulse", label: "Connecting" },
    error: { dot: "bg-rose-500", label: "Disconnected" },
    closed: { dot: "bg-zinc-400", label: "Closed" },
    unavailable: { dot: "bg-rose-700", label: "Unavailable" },
  };
  const m = map[status] ?? map.closed;
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full bg-zinc-100 px-2 py-0.5 text-xs font-medium text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300"
      data-testid="sse-status"
      data-status={status}
    >
      <span className={cn("h-2 w-2 rounded-full", m.dot)} aria-hidden />
      {m.label}
    </span>
  );
}

function filterCutoff(filter: FilterKey, nowMs: number): number {
  switch (filter) {
    case "24h":
      return nowMs - 24 * 60 * 60 * 1000;
    case "7d":
      return nowMs - 7 * 24 * 60 * 60 * 1000;
    case "active":
      return nowMs - 30 * 60 * 1000; // last 30 minutes
    case "all":
    default:
      return 0;
  }
}

function exportCsvUrl(events: FeedEvent[]): string {
  const header = [
    "occurred_at",
    "kind",
    "category",
    "customer",
    "destination",
    "bytes",
  ];
  const body = events.map((e) =>
    [
      e.occurredAt ?? new Date(e.occurredAtMs).toISOString(),
      e.kind,
      e.category,
      e.customerDisplayName,
      e.destinationSummary,
      e.bytes,
    ]
      .map((v) => `"${String(v ?? "").replace(/"/g, '""')}"`)
      .join(","),
  );
  const csv = [header.join(","), ...body].join("\n");
  return `data:text/csv;charset=utf-8,${encodeURIComponent(csv)}`;
}
