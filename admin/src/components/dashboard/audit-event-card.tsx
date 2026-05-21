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
 *
 * Event-kind discrimination (issue #319): the body of the card is
 * rendered by a switch on the canonical proto kind name — not a single
 * fallback line. Customer-bearing kinds (WORKLOAD_*, ABUSE_FLAGGED,
 * EARNINGS_CREDITED) render the customer + destination + bytes block.
 * Non-customer kinds (SCHEDULER_TRANSITION, KEEPALIVE) render an
 * event-shaped row that describes what happened instead of a phantom
 * customer. The string "Customer-X" never appears in the DOM.
 */
export interface AuditEventCardProps {
  event: AuditEvent;
  /** Re-render trigger so relative timestamps tick. */
  nowMs?: number;
  onBlockCategory?: (category: string) => void;
  onBlockCustomer?: (name: string) => void;
  onBlockDestination?: (host: string) => void;
}

/** The set of kinds whose semantic subject is a customer workload. */
const CUSTOMER_KINDS: ReadonlySet<string> = new Set([
  "EVENT_KIND_WORKLOAD_DISPATCHED",
  "EVENT_KIND_WORKLOAD_COMPLETED",
  "EVENT_KIND_WORKLOAD_BLOCKED",
  "EVENT_KIND_ABUSE_FLAGGED",
  "EVENT_KIND_EARNINGS_CREDITED",
]);

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
  const isCustomerKind = kindName !== undefined && CUSTOMER_KINDS.has(kindName);
  const accent =
    isBlocked || isAbuse
      ? "border-destructive/20 bg-destructive/10 dark:border-destructive-foreground dark:bg-destructive/15"
      : kindName === "EVENT_KIND_EARNINGS_CREDITED"
        ? "border-success/20 bg-success/10 dark:border-success/30 dark:bg-success/15"
        : "border-border bg-white dark:border-foreground dark:bg-foreground";

  return (
    <article
      data-testid="audit-event-card"
      data-kind={kindName ?? event.kind}
      className={cn("flex gap-3 rounded-md border p-3 text-sm", accent)}
    >
      <div
        aria-hidden
        className="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full bg-muted font-mono text-base dark:bg-foreground"
      >
        {glyph}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="font-medium">{eventKindLabel(event.kind)}</span>
          {isCustomerKind ? (
            <span
              className="rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium text-foreground dark:bg-foreground dark:text-border"
              data-testid="audit-category"
            >
              {categoryLabel(event.category)}
            </span>
          ) : null}
          <span className="text-xs text-muted-foreground">
            {formatRelativeTime(event.occurredAt, nowMs)}
          </span>
        </div>
        <AuditEventBody event={event} kindName={kindName} />
      </div>
      <div className="flex flex-shrink-0 flex-col gap-1.5">
        {isCustomerKind && event.category && onBlockCategory ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onBlockCategory(event.category)}
            aria-label={`Block category ${event.category}`}
          >
            Block category
          </Button>
        ) : null}
        {isCustomerKind && event.customerDisplayName && onBlockCustomer ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onBlockCustomer(event.customerDisplayName)}
            aria-label={`Block customer ${event.customerDisplayName}`}
          >
            Block customer
          </Button>
        ) : null}
        {isCustomerKind && event.destinationSummary && onBlockDestination ? (
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

/**
 * AuditEventBody renders the per-kind body of the card. Kept separate
 * so the parent card's chrome (icon, label, timestamp pill, action
 * column) can stay declarative while the body branches on event kind.
 */
function AuditEventBody({
  event,
  kindName,
}: {
  event: AuditEvent;
  kindName: string | undefined;
}) {
  switch (kindName) {
    case "EVENT_KIND_WORKLOAD_DISPATCHED":
    case "EVENT_KIND_WORKLOAD_COMPLETED":
    case "EVENT_KIND_WORKLOAD_BLOCKED":
    case "EVENT_KIND_ABUSE_FLAGGED":
    case "EVENT_KIND_EARNINGS_CREDITED":
      return <CustomerEventBody event={event} />;

    case "EVENT_KIND_SCHEDULER_TRANSITION":
      return <SchedulerTransitionBody event={event} />;

    case "EVENT_KIND_KEEPALIVE":
      // KEEPALIVE events are dropped at the SSE proxy (#323) so they
      // should never reach this card in production. If a buggy
      // upstream lets one through, surface it as a debug row rather
      // than a fake customer entry — clicking it is harmless.
      return (
        <p
          className="mt-1 truncate text-xs italic text-muted-foreground"
          data-testid="audit-event-body"
        >
          stream keep-alive
        </p>
      );

    default:
      // Forward-compat: a new EventKind added to the proto but not yet
      // mapped here renders an honest "Event" label and the metadata
      // bag if present. We never invent a customer.
      return <GenericEventBody event={event} />;
  }
}

/**
 * CustomerEventBody renders the canonical customer → destination row
 * for workload + earnings + abuse events. When the customer name is
 * missing on a customer-bearing kind we surface "(unknown customer)"
 * with a warning glyph instead of the previous "Customer-X" fake.
 * That's a real diagnostic, not a placeholder.
 */
function CustomerEventBody({ event }: { event: AuditEvent }) {
  const hasCustomer = Boolean(event.customerDisplayName);
  return (
    <>
      <p
        className="mt-1 truncate text-foreground dark:text-border"
        data-testid="audit-event-body"
      >
        {hasCustomer ? (
          <span className="font-medium">{event.customerDisplayName}</span>
        ) : (
          <span
            className="inline-flex items-center gap-1 text-warning-foreground dark:text-warning/30"
            data-testid="audit-unknown-customer"
            title="Upstream event arrived without a customer display name"
          >
            <span aria-hidden>⚠</span>
            <span className="italic">(unknown customer)</span>
          </span>
        )}
        {event.destinationSummary ? (
          <>
            <span className="mx-1.5 text-muted-foreground">→</span>
            <span className="font-mono text-xs">{event.destinationSummary}</span>
          </>
        ) : null}
      </p>
      {event.bytes && event.bytes !== "0" ? (
        <p className="mt-0.5 text-xs text-muted-foreground">
          {formatBytes(event.bytes)} transferred
        </p>
      ) : null}
    </>
  );
}

/**
 * SchedulerTransitionBody renders the human reason for a scheduler
 * state change. The metadata bag carries the from/to states (see
 * providers-svc's transparency projector) but we fall back to a
 * generic phrase if the projector didn't fill them in.
 */
function SchedulerTransitionBody({ event }: { event: AuditEvent }) {
  const md = event.metadata ?? {};
  const from = md["from"] ?? md["from_state"];
  const to = md["to"] ?? md["to_state"];
  const reason = md["reason"];
  return (
    <p
      className="mt-1 truncate text-foreground dark:text-border"
      data-testid="audit-event-body"
    >
      {from && to ? (
        <>
          Scheduler moved from{" "}
          <span className="font-mono text-xs">{from}</span> to{" "}
          <span className="font-mono text-xs">{to}</span>
        </>
      ) : (
        <span className="italic text-muted-foreground">scheduler state changed</span>
      )}
      {reason ? (
        <>
          <span className="mx-1.5 text-muted-foreground">•</span>
          <span className="text-xs text-muted-foreground">{reason}</span>
        </>
      ) : null}
    </p>
  );
}

/**
 * GenericEventBody renders a future / unknown event kind by surfacing
 * any free-form metadata. Never invents a customer name.
 */
function GenericEventBody({ event }: { event: AuditEvent }) {
  const md = event.metadata ?? {};
  const entries = Object.entries(md);
  if (entries.length === 0) {
    return (
      <p
        className="mt-1 truncate text-xs italic text-muted-foreground"
        data-testid="audit-event-body"
      >
        no additional detail
      </p>
    );
  }
  return (
    <p
      className="mt-1 truncate font-mono text-xs text-foreground dark:text-muted-foreground"
      data-testid="audit-event-body"
    >
      {entries.map(([k, v]) => `${k}=${v}`).join(" ")}
    </p>
  );
}
