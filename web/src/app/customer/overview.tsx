"use client";

import * as React from "react";
import Link from "next/link";
import { StatsCard } from "@/components/dashboard/stats-card";
import { browserApi } from "@/lib/api";
import { formatBytes, formatMoney } from "@/lib/format";
import type { ListUsageResponse } from "@/lib/types";

/**
 * Customer overview pulls aggregate usage from the BFF and renders the
 * spend headline + bytes-by-workload chart. The workspace id is read
 * from localStorage — it's set by the workspace switcher (which lands
 * in a follow-up PR).
 */
export function CustomerOverview() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [usage, setUsage] = React.useState<ListUsageResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [err, setErr] = React.useState<string | null>(null);

  React.useEffect(() => {
    setWsId(localStorage.getItem("iogrid_workspace_id"));
  }, []);

  React.useEffect(() => {
    if (!wsId) {
      setLoading(false);
      return;
    }
    const end = new Date();
    const start = new Date(end.getFullYear(), end.getMonth(), 1);
    const qs = new URLSearchParams({
      workspace_id: wsId,
      start: start.toISOString(),
      end: end.toISOString(),
    });
    browserApi()
      .get<ListUsageResponse>(`/api/v1/customer/usage?${qs.toString()}`)
      .then(setUsage)
      .catch((e) => setErr((e as Error).message))
      .finally(() => setLoading(false));
  }, [wsId]);

  if (!wsId) {
    return <WorkspaceSetupPanel onSave={(v) => setWsId(v)} />;
  }
  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading workspace…
      </div>
    );
  }

  const rows = usage?.rows ?? [];
  const totalCostMicros = rows.reduce(
    (acc, r) => acc + Number(r.costMicros || 0),
    0,
  );
  const totalBytes = rows.reduce((acc, r) => acc + Number(r.bytes || 0), 0);
  const byType = new Map<string, { bytes: number; cost: number }>();
  for (const r of rows) {
    const cur = byType.get(r.workloadType) ?? { bytes: 0, cost: 0 };
    cur.bytes += Number(r.bytes || 0);
    cur.cost += Number(r.costMicros || 0);
    byType.set(r.workloadType, cur);
  }

  return (
    <div className="space-y-6">
      {err ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">
          Couldn&apos;t load usage: {err}
        </div>
      ) : null}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <StatsCard
          label="Spend this month"
          value={formatMoney(totalCostMicros / 1_000_000, "USD")}
          hint={new Date().toLocaleString(undefined, { month: "long", year: "numeric" })}
        />
        <StatsCard
          label="Bytes transferred"
          value={formatBytes(totalBytes)}
          hint="Across all workloads"
        />
        <StatsCard
          label="Workload types"
          value={byType.size}
          hint="Active categories"
        />
      </div>

      <section>
        <h2 className="text-lg font-semibold">Usage by workload type</h2>
        <ul className="mt-3 divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          {byType.size === 0 ? (
            <li className="p-4 text-sm text-zinc-500">
              No usage yet. Submit your first workload through the API or
              the workloads tab.
            </li>
          ) : (
            Array.from(byType.entries()).map(([k, v]) => (
              <li
                key={k}
                className="flex items-center justify-between p-3 text-sm"
              >
                <span>{workloadLabel(k)}</span>
                <span className="font-mono">
                  {formatBytes(v.bytes)} · {formatMoney(v.cost / 1_000_000)}
                </span>
              </li>
            ))
          )}
        </ul>
      </section>

      <section className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <QuickLink
          href="/customer/api-keys"
          label="API keys"
          description="Create and revoke"
        />
        <QuickLink
          href="/customer/workloads"
          label="Workloads"
          description="Live status feed"
        />
        <QuickLink
          href="/customer/billing"
          label="Billing"
          description="Stripe portal"
        />
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

function workloadLabel(k: string): string {
  switch (k) {
    case "WORKLOAD_TYPE_BANDWIDTH":
      return "Bandwidth (residential)";
    case "WORKLOAD_TYPE_DOCKER":
      return "Docker container";
    case "WORKLOAD_TYPE_GPU":
      return "GPU compute";
    case "WORKLOAD_TYPE_IOS_BUILD":
      return "iOS build";
    default:
      return k;
  }
}

function WorkspaceSetupPanel({ onSave }: { onSave: (id: string) => void }) {
  const [val, setVal] = React.useState("");
  return (
    <div className="rounded-md border border-zinc-200 bg-white p-6 dark:border-zinc-800 dark:bg-zinc-900">
      <h2 className="text-lg font-semibold">Pick a workspace</h2>
      <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
        Your customer workspace is identified by a UUID. Paste it below to
        bind this browser session — we&apos;ll persist it in localStorage so you
        only do this once.
      </p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!val) return;
          localStorage.setItem("iogrid_workspace_id", val);
          onSave(val);
        }}
        className="mt-4 flex gap-2"
      >
        <input
          type="text"
          required
          value={val}
          onChange={(e) => setVal(e.target.value)}
          placeholder="00000000-0000-0000-0000-000000000000"
          aria-label="Workspace ID"
          pattern="[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
          className="flex-1 rounded-md border border-zinc-300 bg-transparent px-3 py-2 text-sm font-mono focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-zinc-400 dark:border-zinc-700"
        />
        <button
          type="submit"
          className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900"
        >
          Bind workspace
        </button>
      </form>
    </div>
  );
}
