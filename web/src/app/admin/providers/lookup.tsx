"use client";

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AuditEventCard } from "@/components/dashboard/audit-event-card";
import { browserApi } from "@/lib/api";
import { useSSE } from "@/lib/sse";
import type { AuditEvent } from "@/lib/types";

/**
 * /admin/providers — per-provider transparency audit (#286).
 *
 * The previous version of this component (#238) only surfaced the raw
 * `useSSE` status string ("connecting"), which left the operator
 * staring at "status: connecting" forever for a freshly paired
 * provider with no workload activity. This file rewrites the UX:
 *
 *   - explanatory help block (what an audit event is, who can see it,
 *     retention) — collapsible so power-users can dismiss it
 *   - status pill that renders "live (no events yet)" once the SSE
 *     handshake fires `open`, instead of "connecting" forever
 *   - "Stream opened; waiting for first event." sub-status if the
 *     handshake hasn't fired within 5s (gateways occasionally buffer
 *     the initial response headers — this tells the operator the
 *     stream is alive, just slow)
 *   - rich empty state that explains _why_ no events are showing
 *     yet, with the provider's `registered_at` timestamp as context
 *
 * The SSE backend (BFF route `/api/v1/provide/audit/stream`) is left
 * untouched — this is a pure frontend fix.
 */

type ProviderSummary = {
  id?: { value: string };
  displayName?: string;
  registeredAt?: string;
};

type ListResp = { providers?: ProviderSummary[] };

const PATTERN_UUID =
  "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}";

export function ProviderAuditLookup() {
  const [providerId, setProviderId] = React.useState("");
  const [streamFor, setStreamFor] = React.useState<string | null>(null);
  const [helpOpen, setHelpOpen] = React.useState(true);
  const [slowConnect, setSlowConnect] = React.useState(false);
  const [providerMeta, setProviderMeta] =
    React.useState<ProviderSummary | null>(null);

  const url = React.useMemo(() => {
    if (!streamFor) return null;
    return `${browserApi().baseUrl}/api/v1/provide/audit/stream?provider_id=${streamFor}`;
  }, [streamFor]);

  const { events, status } = useSSE<AuditEvent>({
    url,
    parse: (raw) => {
      try {
        return JSON.parse(raw) as AuditEvent;
      } catch {
        return null;
      }
    },
  });

  // Look up the provider's registered_at so the empty-state copy can
  // tell the operator "paired since <date>, no workloads yet". We
  // intentionally call the same admin list endpoint (#237) that the
  // `<ProviderList>` table above uses — going through the BFF inherits
  // the NextAuth session cookie + ADMIN role assertion.
  React.useEffect(() => {
    if (!streamFor) {
      setProviderMeta(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/v1/admin/providers/list", {
          method: "POST",
          credentials: "include",
          headers: { "content-type": "application/json" },
          body: "{}",
        });
        if (!res.ok) return;
        const json: ListResp = await res.json();
        const match = (json.providers ?? []).find(
          (p) => p.id?.value === streamFor,
        );
        if (!cancelled) setProviderMeta(match ?? null);
      } catch {
        // Best-effort enrichment — the audit stream UX still works
        // without the registered_at context.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [streamFor]);

  // Sub-status: if we're still "connecting" 5s after opening the
  // stream, surface that the handshake is dragging. Most gateways
  // flush response headers immediately, but a buffering proxy in
  // front of the BFF can stall the EventSource `open` event.
  React.useEffect(() => {
    setSlowConnect(false);
    if (!streamFor) return;
    if (status !== "connecting") return;
    const t = setTimeout(() => setSlowConnect(true), 5000);
    return () => clearTimeout(t);
  }, [streamFor, status]);

  return (
    <div className="space-y-4">
      <HelpBlock open={helpOpen} onToggle={() => setHelpOpen((o) => !o)} />

      <form
        onSubmit={(e) => {
          e.preventDefault();
          setStreamFor(providerId.trim() || null);
        }}
        className="flex gap-2"
      >
        <Input
          type="text"
          value={providerId}
          onChange={(e) => setProviderId(e.target.value)}
          placeholder="Provider UUID"
          aria-label="Provider UUID"
          pattern={PATTERN_UUID}
          className="font-mono"
        />
        <Button type="submit">Audit</Button>
      </form>

      {streamFor ? (
        <StatusLine
          streamFor={streamFor}
          status={status}
          slowConnect={slowConnect}
          hasEvents={events.length > 0}
        />
      ) : (
        <p className="text-xs text-zinc-500">
          Enter a provider UUID and press Audit to open their transparency
          stream.
        </p>
      )}

      {streamFor && events.length === 0 && status !== "error" ? (
        <EmptyState
          status={status}
          slowConnect={slowConnect}
          providerMeta={providerMeta}
          providerId={streamFor}
        />
      ) : null}

      {events.length > 0 ? (
        <ul className="space-y-2" data-testid="audit-events">
          {events
            .slice()
            .reverse()
            .slice(0, 50)
            .map((ev, i) => (
              <li key={ev.id?.value ?? i}>
                <AuditEventCard event={ev} />
              </li>
            ))}
        </ul>
      ) : null}
    </div>
  );
}

function HelpBlock({
  open,
  onToggle,
}: {
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 text-sm dark:border-zinc-800 dark:bg-zinc-900/40">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-center justify-between px-3 py-2 text-left font-medium text-zinc-800 dark:text-zinc-100"
      >
        <span>What is an audit event?</span>
        <span aria-hidden className="text-xs text-zinc-500">
          {open ? "Hide" : "Show"}
        </span>
      </button>
      {open ? (
        <div className="space-y-2 border-t border-zinc-200 px-3 py-2 text-xs leading-relaxed text-zinc-700 dark:border-zinc-800 dark:text-zinc-300">
          <p>
            Every workload assignment, completion, failure, or abuse flag a
            provider&apos;s daemon receives is recorded here in real time.
          </p>
          <p>
            Events are visible to global admins only (per #232&apos;s{" "}
            <code className="font-mono">RequireRole(&quot;ADMIN&quot;)</code>
            ). Retention: 90 days, enforced by the transparency CronJob.
          </p>
        </div>
      ) : null}
    </div>
  );
}

function StatusLine({
  streamFor,
  status,
  slowConnect,
  hasEvents,
}: {
  streamFor: string;
  status: "connecting" | "open" | "closed" | "error";
  slowConnect: boolean;
  hasEvents: boolean;
}) {
  // Once the SSE handshake fires `open` we say "live" — and if there
  // are no events yet, qualify that with "(no events yet)". Prior to
  // this fix the component just echoed the raw status string, which
  // left "status: connecting" pinned on screen forever.
  const label = (() => {
    switch (status) {
      case "open":
        return hasEvents ? "live" : "live (no events yet)";
      case "connecting":
        return slowConnect
          ? "connecting — stream opened, waiting for first event"
          : "connecting";
      case "closed":
        return "closed";
      case "error":
        return "disconnected — retrying";
    }
  })();

  return (
    <p
      className="text-xs text-zinc-500"
      data-testid="audit-status-line"
      data-status={status}
    >
      Streaming events for{" "}
      <code className="font-mono">{streamFor}</code> — status: {label}
    </p>
  );
}

function EmptyState({
  status,
  slowConnect,
  providerMeta,
  providerId,
}: {
  status: "connecting" | "open" | "closed" | "error";
  slowConnect: boolean;
  providerMeta: ProviderSummary | null;
  providerId: string;
}) {
  const since =
    providerMeta?.registeredAt &&
    new Date(providerMeta.registeredAt).toLocaleString();

  // While the handshake is mid-flight, render a thin "connecting"
  // skeleton instead of the full empty-state — the latter is for
  // "stream is healthy but the daemon has nothing to show" which is
  // only true after the EventSource open event has fired.
  if (status === "connecting" && !slowConnect) {
    return (
      <div
        className="rounded-md border border-dashed border-zinc-300 p-4 text-sm text-zinc-500 dark:border-zinc-700"
        data-testid="audit-empty-state"
        data-variant="connecting"
      >
        Opening transparency stream…
      </div>
    );
  }

  return (
    <div
      className="space-y-2 rounded-md border border-dashed border-zinc-300 p-4 text-sm dark:border-zinc-700"
      data-testid="audit-empty-state"
      data-variant={status === "open" ? "live" : "stalled"}
    >
      <p className="font-medium text-zinc-800 dark:text-zinc-100">
        No transparency events recorded yet.
      </p>
      <p className="text-zinc-600 dark:text-zinc-400">
        Once this provider accepts a workload, dispatch, completion, failure,
        and abuse-flag events will appear here in real time.
      </p>
      {since ? (
        <p className="text-xs text-zinc-500">
          Provider <code className="font-mono">{providerId}</code> has been
          paired since <span className="font-medium">{since}</span> but has
          not yet accepted any workloads.
        </p>
      ) : (
        <p className="text-xs text-zinc-500">
          Provider <code className="font-mono">{providerId}</code> has no
          workload history on record.
        </p>
      )}
    </div>
  );
}
