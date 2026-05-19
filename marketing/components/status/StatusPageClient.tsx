"use client";

import { useEffect, useMemo, useState } from "react";
import { OverallBanner } from "./OverallBanner";
import { ServiceGrid } from "./ServiceGrid";
import { IncidentList } from "./IncidentList";
import { SubscribeForm } from "./SubscribeForm";
import { UptimeHeatmaps } from "./UptimeHeatmaps";
import type { PostureResponse, StaticIncidentsBundle } from "./types";

interface Props {
  staticIncidents: StaticIncidentsBundle;
  apiBase: string;
}

// Default services rendered in the heatmap row. Each one calls
// /status/uptime?service=<name> on mount. Keep in sync with the SLO
// catalogue under coordinator/services/telemetry-svc/slo/.
const DEFAULT_SERVICES = [
  "proxy-gateway",
  "build-gateway",
  "identity-svc",
  "workloads-svc",
  "billing-svc",
  "vpn-gateway",
] as const;

const POLL_INTERVAL_MS = 60_000;

/**
 * StatusPageClient orchestrates the live status page UI.
 *
 * Lifecycle:
 *  1. Hydrates with the static incident bundle so the page never
 *     shows a blank skeleton on first paint.
 *  2. Fetches /status/posture from the public telemetry-svc endpoint
 *     immediately, then re-fetches every 60 seconds.
 *  3. On fetch failure, keeps the last good payload visible and shows
 *     a discreet "stale" pill rather than throwing the whole page
 *     into an error state — the worst thing a status page can do is
 *     itself look broken.
 */
export function StatusPageClient({ staticIncidents, apiBase }: Props) {
  const [posture, setPosture] = useState<PostureResponse | null>(null);
  const [stale, setStale] = useState(false);
  const [lastFetched, setLastFetched] = useState<Date | null>(null);

  // Seed from static bundle so server-rendered HTML carries a meaningful
  // first frame.
  const fallback = useMemo<PostureResponse>(
    () => ({
      schema_version: staticIncidents.schema_version,
      generated_at: new Date().toISOString(),
      overall: "up",
      services: [],
      incidents_active: staticIncidents.active,
      incidents_recent: staticIncidents.recent,
    }),
    [staticIncidents],
  );

  useEffect(() => {
    let aborted = false;
    const fetchOnce = async () => {
      try {
        const res = await fetch(`${apiBase}/status/posture`, {
          headers: { Accept: "application/json" },
          cache: "no-store",
        });
        if (!res.ok) throw new Error(`status ${res.status}`);
        const body: PostureResponse = await res.json();
        if (aborted) return;
        setPosture(body);
        setLastFetched(new Date());
        setStale(false);
      } catch {
        if (aborted) return;
        setStale(true);
      }
    };
    void fetchOnce();
    const id = setInterval(fetchOnce, POLL_INTERVAL_MS);
    return () => {
      aborted = true;
      clearInterval(id);
    };
  }, [apiBase]);

  const live = posture ?? fallback;
  const services =
    live.services.length > 0
      ? live.services.map((s) => s.name)
      : [...DEFAULT_SERVICES];

  return (
    <div className="bg-neutral-50">
      <section className="container-page py-12 lg:py-16">
        <header className="mb-8 flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="pill mb-3">status.iogrid.org</p>
            <h1 className="h-section">iogrid status</h1>
            <p className="text-lead mt-2 max-w-2xl">
              Live operational view of every iogrid service. SLO posture
              is rolled up from the coordinator&apos;s telemetry-svc and
              refreshed every 60 seconds.
            </p>
          </div>
          <div className="text-right text-xs text-neutral-600">
            <FreshnessPill stale={stale} lastFetched={lastFetched} />
          </div>
        </header>

        <OverallBanner overall={live.overall} />

        <div className="mt-10 grid grid-cols-1 gap-10 lg:grid-cols-3">
          <div className="lg:col-span-2 space-y-10">
            <section aria-labelledby="services-heading">
              <h2 id="services-heading" className="h-card mb-4">
                Services
              </h2>
              <ServiceGrid services={live.services} />
            </section>

            <section aria-labelledby="uptime-heading">
              <h2 id="uptime-heading" className="h-card mb-4">
                90-day uptime
              </h2>
              <UptimeHeatmaps services={services} apiBase={apiBase} />
            </section>

            <section aria-labelledby="incidents-active-heading">
              <h2
                id="incidents-active-heading"
                className="h-card mb-4"
              >
                Active incidents
              </h2>
              <IncidentList
                incidents={live.incidents_active}
                emptyLabel="No active incidents — all systems nominal."
              />
            </section>

            <section aria-labelledby="incidents-recent-heading">
              <h2
                id="incidents-recent-heading"
                className="h-card mb-4"
              >
                Last 7 days
              </h2>
              <IncidentList
                incidents={live.incidents_recent}
                emptyLabel="No incidents in the past 7 days."
              />
            </section>
          </div>

          <aside className="space-y-6">
            <div className="card">
              <h3 className="h-card mb-2 text-lg">Subscribe to updates</h3>
              <p className="text-sm text-neutral-600 mb-4">
                Get an email when an incident is opened or status
                changes.
              </p>
              <SubscribeForm apiBase={apiBase} />
            </div>
            <div className="card">
              <h3 className="h-card mb-2 text-lg">How this page works</h3>
              <ul className="text-sm text-neutral-600 space-y-2">
                <li>
                  SLO posture from the coordinator&apos;s telemetry-svc.
                </li>
                <li>
                  Incidents are operator-curated — created against
                  /status/incidents during outages.
                </li>
                <li>
                  Per-service heatmap rolls up Mimir burn-rate samples
                  into one cell per day.
                </li>
                <li>Auto-refreshes every 60 seconds.</li>
              </ul>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}

function FreshnessPill({
  stale,
  lastFetched,
}: {
  stale: boolean;
  lastFetched: Date | null;
}) {
  if (stale) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-warning/10 px-2 py-0.5 font-semibold text-warning">
        stale data
      </span>
    );
  }
  if (!lastFetched) {
    return <span className="text-neutral-500">loading…</span>;
  }
  return (
    <span>
      updated{" "}
      <time dateTime={lastFetched.toISOString()}>
        {lastFetched.toLocaleTimeString()}
      </time>
    </span>
  );
}
