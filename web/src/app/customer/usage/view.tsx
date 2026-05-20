"use client";

import * as React from "react";
import { toast } from "sonner";
import { EarningsChart } from "@/components/dashboard/earnings-chart";
import { StatsCard } from "@/components/dashboard/stats-card";
import { browserApi } from "@/lib/api";
import { formatBytes, formatMoney } from "@/lib/format";
import { WorkloadTypeNames, protoEnumName } from "@/lib/proto-enum";
import type { ListUsageResponse, UsageRow } from "@/lib/types";

type WorkloadTypeName =
  | "WORKLOAD_TYPE_BANDWIDTH"
  | "WORKLOAD_TYPE_DOCKER"
  | "WORKLOAD_TYPE_GPU"
  | "WORKLOAD_TYPE_IOS_BUILD";

export function UsageView() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [rows, setRows] = React.useState<UsageRow[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [selectedType, setSelectedType] = React.useState<
    WorkloadTypeName | "all"
  >("all");

  React.useEffect(() => {
    setWsId(localStorage.getItem("iogrid_workspace_id"));
  }, []);

  React.useEffect(() => {
    if (!wsId) {
      setLoading(false);
      return;
    }
    const end = new Date();
    const start = new Date(end.getTime() - 30 * 86400_000);
    const qs = new URLSearchParams({
      workspace_id: wsId,
      start: start.toISOString(),
      end: end.toISOString(),
    });
    browserApi()
      .get<ListUsageResponse>(`/api/v1/customer/usage?${qs.toString()}`)
      .then((r) => setRows(r.rows ?? []))
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, [wsId]);

  if (!wsId) {
    return (
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">
        Bind a workspace on the Overview tab first.
      </div>
    );
  }

  // gateway-bff emits enum fields as numeric tags via encoding/json, so
  // canonicalise both sides of the compare to the proto's SCREAMING_SNAKE_CASE
  // form before filtering. See #314.
  const visible =
    selectedType === "all"
      ? rows
      : rows.filter(
          (r) => protoEnumName(r.workloadType, WorkloadTypeNames) === selectedType,
        );

  const totalBytes = visible.reduce((acc, r) => acc + Number(r.bytes || 0), 0);
  const totalCost = visible.reduce(
    (acc, r) => acc + Number(r.costMicros || 0),
    0,
  );

  // Bucket the rows by day for the chart.
  const buckets = new Map<string, number>();
  for (const r of visible) {
    const key = r.bucketStart.slice(0, 10);
    buckets.set(key, (buckets.get(key) ?? 0) + Number(r.bytes || 0));
  }
  const chartPoints = Array.from(buckets.entries())
    .sort(([a], [b]) => (a < b ? -1 : 1))
    .map(([day, bytes]) => ({
      bucket: day.slice(5),
      // Express on a "GB" axis so the chart's currency-formatter
      // (designed for earnings) still produces a useful label.
      amount: bytes / 1024 ** 3,
    }));

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <StatsCard
          label="Bytes (last 30 days)"
          value={formatBytes(totalBytes)}
        />
        <StatsCard
          label="Cost (last 30 days)"
          value={formatMoney(totalCost / 1_000_000)}
        />
        <StatsCard label="Records" value={visible.length} />
      </div>

      <div className="flex items-center gap-2 text-sm">
        <label htmlFor="usage-type">Workload type:</label>
        <select
          id="usage-type"
          value={selectedType}
          onChange={(e) =>
            setSelectedType(e.target.value as WorkloadTypeName | "all")
          }
          className="h-8 rounded-md border border-zinc-300 bg-transparent px-2 text-sm dark:border-zinc-700"
        >
          <option value="all">All</option>
          <option value="WORKLOAD_TYPE_BANDWIDTH">Bandwidth</option>
          <option value="WORKLOAD_TYPE_DOCKER">Docker</option>
          <option value="WORKLOAD_TYPE_GPU">GPU</option>
          <option value="WORKLOAD_TYPE_IOS_BUILD">iOS build</option>
        </select>
      </div>

      {loading ? (
        <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
          Loading…
        </div>
      ) : chartPoints.length === 0 ? (
        <div className="rounded-md border border-dashed border-zinc-300 p-8 text-center text-sm text-zinc-500 dark:border-zinc-700">
          No usage in this window.
        </div>
      ) : (
        <EarningsChart data={chartPoints} currencyCode="USD" />
      )}
    </div>
  );
}
