"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type { AbuseFilterRule, ListFiltersResponse } from "@/lib/types";

export function AbusePanel() {
  const [data, setData] = React.useState<ListFiltersResponse | null>(null);
  const [loading, setLoading] = React.useState(true);

  React.useEffect(() => {
    browserApi()
      .get<ListFiltersResponse>("/api/v1/admin/abuse-queue")
      .then(setData)
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading filter ruleset…
      </div>
    );
  }
  if (!data) {
    return (
      <div className="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950 dark:text-rose-300">
        Couldn&apos;t load — are you signed in with an admin token?
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {data.rulesetHash ? (
        <p className="text-xs text-zinc-500">
          Active ruleset hash:{" "}
          <code className="font-mono">{data.rulesetHash}</code>
        </p>
      ) : null}
      <ul className="divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        {(data.rules ?? []).length === 0 ? (
          <li className="p-4 text-sm text-zinc-500">
            No filter rules loaded.
          </li>
        ) : (
          (data.rules ?? []).map((r) => <RuleRow key={r.id?.value} rule={r} />)
        )}
      </ul>
    </div>
  );
}

function RuleRow({ rule }: { rule: AbuseFilterRule }) {
  const [submitting, setSubmitting] = React.useState(false);
  const decide = async (decision: "allow" | "block") => {
    if (!rule.id?.value) return;
    setSubmitting(true);
    try {
      await browserApi().post(`/api/v1/admin/abuse/${rule.id.value}/resolve`, {
        decision,
        note: "",
      });
      toast.success(`Decision recorded: ${decision}`);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <li className="flex items-center justify-between p-3 text-sm">
      <div className="min-w-0 flex-1">
        <p className="font-medium">
          <code className="font-mono">{rule.pattern}</code>
        </p>
        <p className="text-xs text-zinc-500">
          {rule.kind} · {rule.reason || "no note"} ·{" "}
          {formatRelativeTime(rule.createdAt)}
        </p>
      </div>
      <div className="flex gap-2">
        <Button size="sm" variant="outline" disabled={submitting} onClick={() => decide("allow")}>
          Allow
        </Button>
        <Button
          size="sm"
          className="bg-rose-600 text-white hover:bg-rose-500"
          disabled={submitting}
          onClick={() => decide("block")}
        >
          Block
        </Button>
      </div>
    </li>
  );
}
