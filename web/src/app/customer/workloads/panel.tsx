"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type { UUIDValue, WorkloadType } from "@/lib/types";

interface RecentRow {
  id: string;
  type: WorkloadType;
  destination?: string;
  status: string;
  submittedAt: string;
}

const TYPE_LABELS: Record<string, string> = {
  WORKLOAD_TYPE_BANDWIDTH: "Bandwidth (residential)",
  WORKLOAD_TYPE_DOCKER: "Docker container",
  WORKLOAD_TYPE_GPU: "GPU compute",
  WORKLOAD_TYPE_IOS_BUILD: "iOS build",
};

const TYPES = Object.keys(TYPE_LABELS) as WorkloadType[];

export function WorkloadsPanel() {
  const [type, setType] = React.useState<WorkloadType>("WORKLOAD_TYPE_BANDWIDTH");
  const [destination, setDestination] = React.useState("");
  const [category, setCategory] = React.useState("e_commerce");
  const [submitting, setSubmitting] = React.useState(false);
  const [recent, setRecent] = React.useState<RecentRow[]>([]);
  const [filter, setFilter] = React.useState<string>("all");

  const onSubmit = async () => {
    setSubmitting(true);
    try {
      const res = await browserApi().post<{ workloadId?: UUIDValue }>(
        "/api/v1/customer/workloads",
        {
          workload: {
            type,
            category,
            destination: destination || undefined,
          },
        },
      );
      const id = res.workloadId?.value ?? "(pending)";
      setRecent((r) =>
        [
          {
            id,
            type,
            destination,
            status: "SUBMITTED",
            submittedAt: new Date().toISOString(),
          },
          ...r,
        ].slice(0, 50),
      );
      toast.success("Workload submitted.");
      setDestination("");
    } catch (e) {
      toast.error(`Submit failed: ${(e as Error).message}`);
    } finally {
      setSubmitting(false);
    }
  };

  const filtered =
    filter === "all" ? recent : recent.filter((r) => r.type === filter);

  return (
    <div className="space-y-6">
      <section className="rounded-md border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
        <h2 className="text-sm font-medium">Submit workload</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void onSubmit();
          }}
          className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-3"
        >
          <label className="text-sm">
            <span className="block text-xs font-medium text-zinc-600 dark:text-zinc-400">
              Type
            </span>
            <select
              value={type}
              onChange={(e) => setType(e.target.value as WorkloadType)}
              className="mt-1 h-10 w-full rounded-md border border-zinc-300 bg-transparent px-2 text-sm dark:border-zinc-700"
            >
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {TYPE_LABELS[t]}
                </option>
              ))}
            </select>
          </label>
          <label className="text-sm">
            <span className="block text-xs font-medium text-zinc-600 dark:text-zinc-400">
              Category
            </span>
            <Input
              type="text"
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              placeholder="e_commerce"
              className="mt-1"
            />
          </label>
          <label className="text-sm">
            <span className="block text-xs font-medium text-zinc-600 dark:text-zinc-400">
              Destination
            </span>
            <Input
              type="text"
              value={destination}
              onChange={(e) => setDestination(e.target.value)}
              placeholder="api.example.com"
              className="mt-1"
            />
          </label>
          <div className="md:col-span-3">
            <Button type="submit" disabled={submitting}>
              {submitting ? "Submitting…" : "Submit workload"}
            </Button>
          </div>
        </form>
      </section>

      <section>
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-medium">Recent dispatches</h2>
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label="Filter by type"
            className="h-8 rounded-md border border-zinc-300 bg-transparent px-2 text-xs dark:border-zinc-700"
          >
            <option value="all">All types</option>
            {TYPES.map((t) => (
              <option key={t} value={t}>
                {TYPE_LABELS[t]}
              </option>
            ))}
          </select>
        </div>
        <ul className="mt-3 divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          {filtered.length === 0 ? (
            <li className="p-4 text-sm text-zinc-500">
              Nothing submitted from this browser yet. Workloads queued via
              the API show up in /customer/usage with full historical
              accounting once they bill.
            </li>
          ) : (
            filtered.map((r) => (
              <li key={r.id} className="flex items-center justify-between p-3 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="font-medium">{TYPE_LABELS[r.type] ?? r.type}</p>
                  {r.destination ? (
                    <p className="text-xs text-zinc-500">
                      → <span className="font-mono">{r.destination}</span>
                    </p>
                  ) : null}
                </div>
                <div className="text-right">
                  <p className="text-xs font-medium text-zinc-600 dark:text-zinc-400">
                    {r.status}
                  </p>
                  <p className="text-xs text-zinc-500">
                    {formatRelativeTime(r.submittedAt)}
                  </p>
                </div>
              </li>
            ))
          )}
        </ul>
      </section>
    </div>
  );
}
