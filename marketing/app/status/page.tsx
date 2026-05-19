import type { Metadata } from "next";
import { StatusPageClient } from "@/components/status/StatusPageClient";
import incidentsStatic from "@/content/status/incidents-static.json";

export const metadata: Metadata = {
  title: "Status",
  description:
    "Live operational status for iogrid services. SLO posture, incident history, and 90-day uptime per service — updated every 60 seconds.",
  openGraph: {
    title: "iogrid status",
    description:
      "Live operational status for iogrid services. SLO posture, incidents, 90-day uptime.",
  },
  alternates: { canonical: "/status/" },
  robots: { index: true, follow: true },
};

// status.iogrid.org is rendered statically at build time (the rest of the
// marketing site uses `output: "export"`), then hydrated on the client to
// poll /status/posture every 60s. We pre-render a baseline "up" frame
// so the page never shows a blank loading state to first-paint viewers.
export default function StatusPage() {
  return (
    <StatusPageClient
      staticIncidents={incidentsStatic}
      // Public telemetry-svc endpoint. Configurable per-env via build
      // arg so a preview deploy can point at staging.
      apiBase={process.env.NEXT_PUBLIC_STATUS_API ?? "https://api.iogrid.org"}
    />
  );
}
