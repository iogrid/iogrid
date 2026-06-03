"use client";

import { useEffect, useState } from "react";

/**
 * StatusPageClient (#674) — the live island on /status.
 *
 * Polls the same-origin /status/feed (a thin server proxy onto
 * gateway-bff → telemetry-svc's posture generator) every 30s and
 * renders: overall banner, per-service posture rows with SLO budget,
 * and active/recent incidents. Degrades to an explicit "feed
 * unavailable" card — never a fake all-green.
 */

interface ServicePosture {
  name: string;
  status: "up" | "degraded" | "down" | string;
  slo_percent: number;
}

interface Incident {
  id?: string;
  title?: string;
  severity?: string;
  status?: string;
  started_at?: string;
  updates?: { at?: string; body?: string }[];
}

interface PostureResponse {
  schema_version: number;
  generated_at: string;
  overall: { status?: string; summary?: string };
  services: ServicePosture[];
  incidents_active: Incident[];
  incidents_recent: Incident[];
}

const POLL_MS = 30_000;

const STATUS_STYLES: Record<string, { dot: string; label: string }> = {
  up: { dot: "bg-emerald-500", label: "Operational" },
  degraded: { dot: "bg-amber-500", label: "Degraded" },
  down: { dot: "bg-red-500", label: "Down" },
};

function statusStyle(status: string | undefined) {
  return (
    STATUS_STYLES[status ?? ""] ?? { dot: "bg-muted-foreground", label: status ?? "Unknown" }
  );
}

export function StatusPageClient() {
  const [posture, setPosture] = useState<PostureResponse | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await fetch("/status/feed", { cache: "no-store" });
        if (!res.ok) throw new Error(String(res.status));
        const body = (await res.json()) as PostureResponse;
        if (!cancelled) {
          setPosture(body);
          setFailed(false);
        }
      } catch {
        if (!cancelled) setFailed(true);
      }
    };
    void load();
    const t = setInterval(load, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  if (failed && !posture) {
    return (
      <div
        data-testid="status-feed-unavailable"
        className="rounded-lg border border-border bg-muted/40 p-6 text-sm text-muted-foreground"
      >
        The status feed is currently unreachable from this page. The raw API
        health check at{" "}
        <a className="underline" href="https://api.iogrid.org/healthz">
          api.iogrid.org/healthz
        </a>{" "}
        may still answer.
      </div>
    );
  }

  if (!posture) {
    return (
      <div className="rounded-lg border border-border p-6 text-sm text-muted-foreground">
        Loading live status…
      </div>
    );
  }

  const overall = statusStyle(posture.overall?.status);
  const active = posture.incidents_active ?? [];
  const recent = posture.incidents_recent ?? [];

  return (
    <div className="space-y-8" data-testid="status-dashboard">
      {/* Overall banner */}
      <div className="flex items-center gap-3 rounded-lg border border-border p-5">
        <span className={`h-3 w-3 rounded-full ${overall.dot}`} aria-hidden />
        <div>
          <p className="text-base font-medium">
            {posture.overall?.status === "up"
              ? "All systems operational"
              : (posture.overall?.summary ?? overall.label)}
          </p>
          <p className="text-xs text-muted-foreground">
            Updated {new Date(posture.generated_at).toLocaleTimeString()} ·
            refreshes every 30s
          </p>
        </div>
      </div>

      {/* Per-service rows */}
      <ul className="divide-y divide-border rounded-lg border border-border">
        {(posture.services ?? []).map((svc) => {
          const s = statusStyle(svc.status);
          return (
            <li
              key={svc.name}
              className="flex items-center justify-between px-5 py-3"
            >
              <span className="flex items-center gap-3 text-sm">
                <span className={`h-2 w-2 rounded-full ${s.dot}`} aria-hidden />
                {svc.name}
              </span>
              <span className="flex items-baseline gap-4">
                <span className="text-xs tabular-nums text-muted-foreground">
                  SLO budget {svc.slo_percent.toFixed(1)}%
                </span>
                <span className="text-xs font-medium">{s.label}</span>
              </span>
            </li>
          );
        })}
      </ul>

      {/* Incidents */}
      <div>
        <h2 className="mb-3 text-sm font-medium">Incidents</h2>
        {active.length === 0 && recent.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No active incidents. Nothing reported recently.
          </p>
        ) : (
          <ul className="space-y-3">
            {[...active, ...recent].map((inc, i) => (
              <li
                key={inc.id ?? i}
                className="rounded-lg border border-border p-4"
              >
                <p className="text-sm font-medium">
                  {inc.title ?? "Incident"}
                  {active.includes(inc) ? (
                    <span className="ml-2 rounded bg-amber-500/15 px-1.5 py-0.5 text-xs text-amber-600">
                      active
                    </span>
                  ) : null}
                </p>
                {inc.started_at ? (
                  <p className="mt-1 text-xs text-muted-foreground">
                    Started {new Date(inc.started_at).toLocaleString()}
                  </p>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
