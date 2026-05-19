"use client";

import { useEffect, useState } from "react";
import type { UptimeResponse, UptimeSample } from "./types";

interface Props {
  services: string[];
  apiBase: string;
}

// State -> cell colour. Empty states render as a faint neutral square
// so the calendar layout stays a fixed grid.
const STATE_CLASS: Record<UptimeSample["state"], string> = {
  "": "bg-neutral-200/40",
  op: "bg-success/80",
  deg: "bg-warning",
  down: "bg-danger",
  maint: "bg-info",
};

const STATE_LABEL: Record<UptimeSample["state"], string> = {
  "": "no data",
  op: "operational",
  deg: "degraded",
  down: "outage",
  maint: "planned maintenance",
};

// Friendly per-service display names. Kept duplicated rather than
// imported from ServiceGrid to keep the components decoupled (the SDK
// boundary is the JSON shape, not internal lookup tables).
const NAMES: Record<string, string> = {
  "proxy-gateway": "Bandwidth proxy",
  "build-gateway": "iOS build CI",
  "identity-svc": "Identity & auth",
  "workloads-svc": "Workload dispatch",
  "billing-svc": "Billing & payouts",
  "vpn-gateway": "Consumer VPN",
};

export function UptimeHeatmaps({ services, apiBase }: Props) {
  return (
    <div className="card">
      <div className="space-y-6">
        {services.map((svc) => (
          <ServiceHeatmap key={svc} service={svc} apiBase={apiBase} />
        ))}
      </div>
      <Legend />
    </div>
  );
}

function ServiceHeatmap({ service, apiBase }: { service: string; apiBase: string }) {
  const [samples, setSamples] = useState<UptimeSample[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let aborted = false;
    (async () => {
      try {
        const res = await fetch(
          `${apiBase}/status/uptime?service=${encodeURIComponent(service)}&days=90`,
          { headers: { Accept: "application/json" }, cache: "no-store" },
        );
        if (!res.ok) throw new Error(`status ${res.status}`);
        const body: UptimeResponse = await res.json();
        if (aborted) return;
        setSamples(body.samples ?? []);
      } catch {
        if (aborted) return;
        // Empty samples — the placeholder grid still renders.
        setSamples(emptyDays(90, service));
      } finally {
        if (!aborted) setLoaded(true);
      }
    })();
    return () => {
      aborted = true;
    };
  }, [service, apiBase]);

  const filled = samples.length > 0 ? samples : emptyDays(90, service);
  const uptimePct = computeUptime(filled);

  return (
    <div>
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-sm font-semibold text-neutral-800">
          {NAMES[service] ?? service}
          <span className="ml-2 font-mono text-xs text-neutral-500">
            {service}
          </span>
        </p>
        <p className="text-xs text-neutral-500 font-tabular">
          {loaded ? `${uptimePct.toFixed(2)}% / 90 days` : "loading…"}
        </p>
      </div>
      <div
        role="img"
        aria-label={`${NAMES[service] ?? service}: 90-day uptime heatmap`}
        className="flex gap-[2px]"
      >
        {filled.map((s) => (
          <span
            key={s.day}
            title={`${s.day}: ${STATE_LABEL[s.state]}${s.sli_pct ? ` (${s.sli_pct.toFixed(2)}%)` : ""}`}
            className={`h-6 flex-1 rounded-sm ${STATE_CLASS[s.state] ?? STATE_CLASS[""]}`}
          />
        ))}
      </div>
    </div>
  );
}

function Legend() {
  const items: Array<{ state: UptimeSample["state"]; label: string }> = [
    { state: "op", label: "Operational" },
    { state: "deg", label: "Degraded" },
    { state: "down", label: "Outage" },
    { state: "maint", label: "Maintenance" },
    { state: "", label: "No data" },
  ];
  return (
    <ul className="mt-6 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-neutral-600">
      {items.map((i) => (
        <li key={i.state || "none"} className="flex items-center gap-1.5">
          <span
            className={`inline-block h-3 w-3 rounded-sm ${STATE_CLASS[i.state]}`}
            aria-hidden="true"
          />
          {i.label}
        </li>
      ))}
    </ul>
  );
}

function emptyDays(n: number, service: string): UptimeSample[] {
  const out: UptimeSample[] = [];
  const today = new Date();
  today.setUTCHours(0, 0, 0, 0);
  for (let i = n - 1; i >= 0; i--) {
    const d = new Date(today);
    d.setUTCDate(d.getUTCDate() - i);
    out.push({ service, day: d.toISOString().slice(0, 10), state: "", sli_pct: 0 });
  }
  return out;
}

function computeUptime(samples: UptimeSample[]): number {
  let counted = 0;
  let good = 0;
  for (const s of samples) {
    if (s.state === "") continue;
    counted++;
    if (s.state === "op" || s.state === "maint") good++;
    else if (s.state === "deg") good += 0.9; // partial credit
  }
  if (counted === 0) return 100;
  return (good / counted) * 100;
}
