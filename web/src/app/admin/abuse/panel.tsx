"use client";

import * as React from "react";
import { toast } from "sonner";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type { AbuseFilterRule, ListFiltersResponse } from "@/lib/types";

/**
 * Coerce the proto-generated `last_updated_at` field into something
 * React can safely render. gateway-bff serialises responses via Go's
 * stdlib `encoding/json` rather than `protojson`, so a
 * `*timestamppb.Timestamp` lands on the wire as the struct
 * `{"seconds": N, "nanos": M}` — NOT an RFC3339 string (#304). Pass
 * that object straight into JSX and React stringifies it to the
 * notorious literal `[object Object]`.
 *
 * Accept any of:
 *   - undefined / null      → "" (collapses to em-dash downstream)
 *   - string (RFC3339)      → returned as-is (protojson future-proof)
 *   - { seconds, nanos }    → converted to RFC3339
 */
function timestampToIso(value: unknown): string {
  if (value == null) return "";
  if (typeof value === "string") return value;
  if (typeof value === "object") {
    const v = value as { seconds?: string | number; nanos?: number };
    const secs = typeof v.seconds === "string" ? Number(v.seconds) : v.seconds;
    if (typeof secs === "number" && Number.isFinite(secs)) {
      const ms = secs * 1000 + Math.floor((v.nanos ?? 0) / 1e6);
      const d = new Date(ms);
      if (!Number.isNaN(d.getTime())) return d.toISOString();
    }
  }
  return "";
}

/**
 * AbusePanel renders the antiabuse-svc filter ruleset that gateway-bff
 * exposes via GET /api/v1/admin/abuse-queue. Three render states:
 *
 *   - loading  → centred "Loading filter ruleset…" placeholder
 *   - error    → red-toned banner explaining the failure
 *   - loaded   → either the empty-state card (no events / no rules) or
 *                the live ruleset as a divided list of rule rows
 *
 * The proto's "abuse queue" of yellow-flagged events is not yet defined
 * (see `iogrid.antiabuse.v1.AbuseFilterService` — no `ListEvents` RPC
 * exists in Phase 0). Until it does, the empty events queue is the
 * normal state and we surface that explicitly via the empty-state card
 * so on-call doesn't confuse it with a broken page (#298).
 */
export function AbusePanel() {
  const [data, setData] = React.useState<ListFiltersResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    browserApi()
      .get<ListFiltersResponse>("/api/v1/admin/abuse-queue")
      .then((d) => setData(d ?? { rules: [] }))
      .catch((e) => {
        const msg = (e as Error).message;
        setError(msg);
        toast.error(msg);
      })
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading filter ruleset…
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950 dark:text-rose-300">
        Couldn&apos;t load — are you signed in with an admin token?
      </div>
    );
  }

  const rules = data.rules ?? [];

  if (rules.length === 0) {
    return <EmptyQueueCard />;
  }

  return (
    <div className="space-y-4">
      {data.ruleset_hash ? (
        <p className="text-xs text-zinc-500">
          Active ruleset hash:{" "}
          <code className="font-mono">{data.ruleset_hash}</code>
        </p>
      ) : null}
      <ul className="divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        {rules.map((r) => (
          <RuleRow key={r.id || r.slug} rule={r} />
        ))}
      </ul>
    </div>
  );
}

/**
 * EmptyQueueCard is rendered when the BFF returns zero rules / events.
 * The copy is the one founder-approved on #298: explains what would
 * appear here, who flags it, and the 30-day retention boundary.
 */
function EmptyQueueCard() {
  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 p-6 text-sm dark:border-zinc-800 dark:bg-zinc-900/40">
      <p className="font-medium text-zinc-900 dark:text-zinc-100">
        No abuse events in the queue.
      </p>
      <p className="mt-2 text-zinc-600 dark:text-zinc-400">
        Events flagged by antiabuse-svc (CSAM hashes, phishing URLs,
        sanctions list hits) land here for global-admin review.
        Retention: 30 days.
      </p>
    </div>
  );
}

/**
 * RuleRow renders one active filter rule. Fields map 1:1 onto
 * `iogrid.antiabuse.v1.FilterRule` (snake_case JSON from
 * protoc-gen-go). Missing optional fields collapse to an em-dash; we
 * never render skeleton-shaped rows that imply data we don't have.
 */
function RuleRow({ rule }: { rule: AbuseFilterRule }) {
  const iso = timestampToIso(rule.last_updated_at);
  return (
    <li className="flex items-center justify-between p-3 text-sm">
      <div className="min-w-0 flex-1">
        <p className="font-medium text-zinc-900 dark:text-zinc-100">
          <code className="font-mono">{rule.slug || rule.id || "—"}</code>
        </p>
        <p className="mt-0.5 text-xs text-zinc-500">
          {rule.description || "—"}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-3 pl-4 text-xs text-zinc-500">
        {rule.version ? (
          <span className="rounded bg-zinc-100 px-2 py-0.5 font-mono dark:bg-zinc-800">
            v{rule.version}
          </span>
        ) : null}
        <span title={iso}>{formatRelativeTime(iso || undefined)}</span>
      </div>
    </li>
  );
}
