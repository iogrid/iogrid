"use client";

import * as React from "react";
import { formatRelativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { AuditEvent } from "@/lib/types";

/**
 * AuditEventCard — admin-flavoured row of the live transparency feed.
 *
 * Slim cousin of `web/src/components/dashboard/audit-event-card.tsx`.
 * The provider-facing card carries block-category / block-customer /
 * block-destination buttons; the admin surface VIEWS provider audit
 * feeds for compliance — it never blocks on behalf of a provider, so
 * the action column is intentionally absent.
 *
 * We tolerate both the canonical SCREAMING_SNAKE_CASE event-kind name
 * (proto3-JSON future) and the raw numeric tag Go's `encoding/json`
 * emits today (#314). Falling back to a generic "Audit event" label
 * keeps the card readable while #314's enum-name plumbing matures.
 */
export interface AuditEventCardProps {
  event: AuditEvent;
  nowMs?: number;
}

const EVENT_KIND_LABELS: Record<string, string> = {
  EVENT_KIND_WORKLOAD_DISPATCHED: "Workload dispatched",
  EVENT_KIND_WORKLOAD_COMPLETED: "Workload completed",
  EVENT_KIND_WORKLOAD_BLOCKED: "Workload blocked",
  EVENT_KIND_SCHEDULER_TRANSITION: "Scheduler transition",
  EVENT_KIND_ABUSE_FLAGGED: "Abuse flagged",
  EVENT_KIND_EARNINGS_CREDITED: "Earnings credited",
};

function eventKindName(raw: string | number | undefined): string | undefined {
  if (typeof raw === "string") return raw;
  return undefined;
}

function eventKindLabel(raw: string | number | undefined): string {
  const name = eventKindName(raw);
  if (name && EVENT_KIND_LABELS[name]) return EVENT_KIND_LABELS[name];
  return "Audit event";
}

function formatBytes(input: string | number | undefined): string | null {
  if (input == null || input === "") return null;
  const n = typeof input === "string" ? Number(input) : input;
  if (!Number.isFinite(n) || n <= 0) return null;
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let val = n;
  let unit = 0;
  while (val >= 1024 && unit < units.length - 1) {
    val /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : 1;
  return `${val.toFixed(digits)} ${units[unit]}`;
}

export function AuditEventCard({ event, nowMs }: AuditEventCardProps) {
  const name = eventKindName(event.kind);
  const isBlocked = name === "EVENT_KIND_WORKLOAD_BLOCKED";
  const isAbuse = name === "EVENT_KIND_ABUSE_FLAGGED";
  const accent =
    isBlocked || isAbuse
      ? "border-rose-200 bg-rose-50 dark:border-rose-900 dark:bg-rose-950"
      : name === "EVENT_KIND_EARNINGS_CREDITED"
        ? "border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950"
        : "border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900";

  const bytes = formatBytes(event.bytes);

  return (
    <article
      data-testid="audit-event-card"
      data-kind={name ?? event.kind}
      className={cn("flex gap-3 rounded-md border p-3 text-sm", accent)}
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="font-medium">{eventKindLabel(event.kind)}</span>
          {event.category ? (
            <span
              className="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] font-medium text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300"
              data-testid="audit-category"
            >
              {event.category}
            </span>
          ) : null}
          <span className="text-xs text-zinc-500">
            {formatRelativeTime(event.occurredAt, nowMs)}
          </span>
        </div>
        <p className="mt-1 truncate text-zinc-700 dark:text-zinc-300">
          {event.customerDisplayName ? (
            <span className="font-medium">{event.customerDisplayName}</span>
          ) : null}
          {event.destinationSummary ? (
            <>
              {event.customerDisplayName ? (
                <span className="mx-1.5 text-zinc-400">→</span>
              ) : null}
              <span className="font-mono text-xs">
                {event.destinationSummary}
              </span>
            </>
          ) : null}
        </p>
        {bytes ? (
          <p className="mt-0.5 text-xs text-zinc-500">{bytes} transferred</p>
        ) : null}
      </div>
    </article>
  );
}
