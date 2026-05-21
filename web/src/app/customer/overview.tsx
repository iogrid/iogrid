"use client";

import * as React from "react";
import Link from "next/link";
import { StatsCard } from "@/components/dashboard/stats-card";
import { browserApi } from "@/lib/api";
import { formatBytes, formatMoney } from "@/lib/format";
import { WorkloadTypeNames, protoEnumName } from "@/lib/proto-enum";
import type { ListUsageResponse } from "@/lib/types";

/**
 * Customer overview pulls aggregate usage from the BFF and renders the
 * spend headline + bytes-by-workload chart.
 *
 * Workspace bootstrap (#232): on first render after sign-in we read
 * `iogrid_workspace_id` from localStorage; if missing, we call the BFF
 * `/api/customer/workspaces/init` proxy which returns the user's first
 * workspace (creating one on the fly if they have none). The resolved
 * id is then persisted so subsequent loads skip the round-trip. The
 * legacy "paste a UUID" form remains as a collapsed escape hatch the
 * user can expand if auto-init is unavailable (e.g. identity-svc not
 * yet reachable in a brand-new cluster).
 */
const WORKSPACE_STORAGE_KEY = "iogrid_workspace_id";

export function CustomerOverview() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [bootstrapState, setBootstrapState] = React.useState<
    "loading" | "done" | "fallback"
  >("loading");
  const [bootstrapErr, setBootstrapErr] = React.useState<string | null>(null);
  const [usage, setUsage] = React.useState<ListUsageResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [err, setErr] = React.useState<string | null>(null);

  // First-render bootstrap: read cache → auto-init → fallback.
  React.useEffect(() => {
    let cancelled = false;
    const cached =
      typeof window !== "undefined"
        ? window.localStorage.getItem(WORKSPACE_STORAGE_KEY)
        : null;
    if (cached) {
      setWsId(cached);
      setBootstrapState("done");
      return;
    }
    setBootstrapState("loading");
    void (async () => {
      try {
        const res = await fetch("/api/customer/workspaces/init", {
          method: "POST",
          credentials: "include",
        });
        if (!res.ok) {
          // 503 = auto-init not wired (Phase 0 stop-gap). Fall back to
          // the manual paste form so the user is not blocked.
          if (cancelled) return;
          setBootstrapState("fallback");
          if (res.status !== 503) {
            try {
              const body = (await res.json()) as { message?: string };
              setBootstrapErr(body?.message ?? `HTTP ${res.status}`);
            } catch {
              setBootstrapErr(`HTTP ${res.status}`);
            }
          }
          return;
        }
        const body = (await res.json()) as { workspace_id?: string };
        if (!body?.workspace_id) {
          if (cancelled) return;
          setBootstrapState("fallback");
          setBootstrapErr("init proxy returned no workspace_id");
          return;
        }
        try {
          window.localStorage.setItem(
            WORKSPACE_STORAGE_KEY,
            body.workspace_id,
          );
        } catch {
          // Quota / privacy mode — non-fatal; in-memory id still works.
        }
        if (cancelled) return;
        setWsId(body.workspace_id);
        setBootstrapState("done");
      } catch (e) {
        if (cancelled) return;
        setBootstrapState("fallback");
        setBootstrapErr((e as Error).message);
      }
    })();
    return () => {
      cancelled = true;
    };
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

  if (bootstrapState === "loading" && !wsId) {
    return (
      <div
        className="rounded-md border border-border p-8 text-center text-sm text-muted-foreground dark:border-border"
        data-testid="workspace-bootstrap-loading"
      >
        Setting up your workspace…
      </div>
    );
  }

  if (!wsId) {
    // Auto-init declined (503) or errored — let the user paste a UUID
    // as a fallback so they're never fully blocked.
    return (
      <WorkspaceSetupPanel
        message={bootstrapErr}
        onSave={(v) => {
          try {
            window.localStorage.setItem(WORKSPACE_STORAGE_KEY, v);
          } catch {
            /* non-fatal */
          }
          setWsId(v);
          setBootstrapState("done");
        }}
      />
    );
  }
  if (loading) {
    return (
      <div className="rounded-md border border-border p-8 text-center text-sm text-muted-foreground dark:border-border">
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
  // gateway-bff serialises Connect-Go proto structs with encoding/json,
  // so r.workloadType arrives as a numeric tag. Canonicalise to the
  // SCREAMING_SNAKE_CASE name once so the aggregation key + downstream
  // label switch stay consistent. See #314.
  const byType = new Map<string, { bytes: number; cost: number }>();
  for (const r of rows) {
    const key =
      protoEnumName(r.workloadType, WorkloadTypeNames) ??
      "WORKLOAD_TYPE_UNSPECIFIED";
    const cur = byType.get(key) ?? { bytes: 0, cost: 0 };
    cur.bytes += Number(r.bytes || 0);
    cur.cost += Number(r.costMicros || 0);
    byType.set(key, cur);
  }

  return (
    <div className="space-y-6" data-testid="customer-dashboard">
      {err ? (
        <div className="rounded-md border border-warning/30 bg-warning/10 p-3 text-sm text-warning dark:border-warning/40 dark:bg-warning/15 dark:text-warning">
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
        <ul className="mt-3 divide-y divide-border rounded-md border border-border dark:divide-border dark:border-border">
          {byType.size === 0 ? (
            <li className="p-4 text-sm text-muted-foreground">
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
      className="rounded-md border border-border bg-card p-4 transition-colors hover:border-foreground/40 dark:border-border"
    >
      <p className="text-sm font-medium">{label}</p>
      <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
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

/**
 * Fallback panel shown when auto-init declines (identity-svc not wired)
 * or errors. The paste-UUID form is hidden inside a collapsed `<details>`
 * so it never appears as the primary surface in the happy path; the
 * outer panel explains what's happening so the user is not confused.
 */
function WorkspaceSetupPanel({
  onSave,
  message,
}: {
  onSave: (id: string) => void;
  message?: string | null;
}) {
  const [val, setVal] = React.useState("");
  return (
    <div
      className="rounded-md border border-border bg-card p-6 dark:border-border"
      data-testid="workspace-setup-fallback"
    >
      <h2 className="text-lg font-semibold">Workspace setup</h2>
      <p className="mt-1 text-sm text-muted-foreground dark:text-muted-foreground">
        We tried to auto-create a workspace for you but the identity service
        was not reachable. You can either retry the page, or paste an
        existing workspace UUID below as a one-time fallback.
      </p>
      {message ? (
        <p className="mt-2 text-xs text-muted-foreground dark:text-muted-foreground">
          Detail: {message}
        </p>
      ) : null}
      <details className="mt-4 group">
        <summary className="cursor-pointer text-sm font-medium text-foreground hover:text-foreground dark:text-muted-foreground dark:hover:text-foreground">
          Paste a workspace UUID instead
        </summary>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!val) return;
            onSave(val);
          }}
          className="mt-3 flex gap-2"
        >
          <input
            type="text"
            required
            value={val}
            onChange={(e) => setVal(e.target.value)}
            placeholder="00000000-0000-0000-0000-000000000000"
            aria-label="Workspace ID"
            pattern="[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
            className="flex-1 rounded-md border border-border-strong bg-transparent px-3 py-2 text-sm font-mono focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-strong dark:border-border-strong"
          />
          <button
            type="submit"
            className="rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background hover:bg-foreground/80 dark:bg-foreground dark:text-background"
          >
            Bind workspace
          </button>
        </form>
      </details>
    </div>
  );
}
