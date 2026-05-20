"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import {
  categoryLabel,
  eventKindGlyph,
  eventKindLabel,
  formatBytes,
  formatRelativeTime,
} from "@/lib/format";
import { EventKindNames, protoEnumName } from "@/lib/proto-enum";
import { cn } from "@/lib/utils";
import type { AuditEvent } from "@/lib/types";

/**
 * AuditEventCard — one row of the live transparency feed.
 *
 * Three one-click controls (block category / customer / destination) sit
 * on the right. The handlers are passed in so the parent screen can wire
 * them up to mutations against /api/v1/provide/schedule.
 */
export interface AuditEventCardProps {
  event: AuditEvent;
  /** Re-render trigger so relative timestamps tick. */
  nowMs?: number;
  onBlockCategory?: (category: string) => void;
  onBlockCustomer?: (name: string) => void;
  onBlockDestination?: (host: string) => void;
}

export function AuditEventCard({
  event,
  nowMs,
  onBlockCategory,
  onBlockCustomer,
  onBlockDestination,
}: AuditEventCardProps) {
  // gateway-bff emits proto enums as numeric tags via encoding/json, so
  // normalise to the canonical SCREAMING_SNAKE_CASE name once and branch
  // on the result. See #314.
  const kindName = protoEnumName(event.kind, EventKindNames);
  const glyph = eventKindGlyph(event.kind);
  const isBlocked = kindName === "EVENT_KIND_WORKLOAD_BLOCKED";
  const isAbuse = kindName === "EVENT_KIND_ABUSE_FLAGGED";
  const accent =
    isBlocked || isAbuse
      ? "border-rose-200 bg-rose-50 dark:border-rose-900 dark:bg-rose-950"
      : kindName === "EVENT_KIND_EARNINGS_CREDITED"
        ? "border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950"
        : "border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900";

  return (
    <article
      data-testid="audit-event-card"
      data-kind={kindName ?? event.kind}
      className={cn("flex gap-3 rounded-md border p-3 text-sm", accent)}
    >
      <div
        aria-hidden
        className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full bg-zinc-100 font-mono text-base dark:bg-zinc-800"
      >
        {glyph}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="font-medium">{eventKindLabel(event.kind)}</span>
          <span
            className="rounded-full bg-zinc-100 px-2 py-0.5 text-[11px] font-medium text-zinc-700 dark:bg-zinc-800 dark:text-zinc-300"
            data-testid="audit-category"
          >
            {categoryLabel(event.category)}
          </span>
          <span className="text-xs text-zinc-500">
            {formatRelativeTime(event.occurredAt, nowMs)}
          </span>
        </div>
        <p className="mt-1 truncate text-zinc-700 dark:text-zinc-300">
          <span className="font-medium">
            {event.customerDisplayName || "Customer-X"}
          </span>
          {event.destinationSummary ? (
            <>
              <span className="mx-1.5 text-zinc-400">→</span>
              <span className="font-mono text-xs">{event.destinationSummary}</span>
            </>
          ) : null}
        </p>
        {event.bytes && event.bytes !== "0" ? (
          <p className="mt-0.5 text-xs text-zinc-500">
            {formatBytes(event.bytes)} transferred
          </p>
        ) : null}
      </div>
      <div className="flex flex-shrink-0 flex-col gap-1.5">
        {event.category && onBlockCategory ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onBlockCategory(event.category)}
            aria-label={`Block category ${event.category}`}
          >
            Block category
          </Button>
        ) : null}
        {event.customerDisplayName && onBlockCustomer ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onBlockCustomer(event.customerDisplayName)}
            aria-label={`Block customer ${event.customerDisplayName}`}
          >
            Block customer
          </Button>
        ) : null}
        {event.destinationSummary && onBlockDestination ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onBlockDestination(event.destinationSummary)}
            aria-label={`Block destination ${event.destinationSummary}`}
          >
            Block destination
          </Button>
        ) : null}
      </div>
    </article>
  );
}
