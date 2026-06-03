import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Status",
  description:
    "iogrid operational status — live API health check and incident updates.",
};

/**
 * Status landing — folded from marketing/app/status/page.tsx into
 * web/'s design system during EPIC #422 Phase 3.
 *
 * NOTE (#668, 2026-06-03): this page previously linked to a dedicated
 * `status.iogrid.org` subdomain dashboard, but that subdomain was never
 * actually served — its routing is a Gateway-API HTTPRoute
 * (`gateways/httproute-status.yaml`) which is inert because the cluster
 * routes via Traefik, and no status backend was deployed, so the link
 * dead-ended at a raw `404 page not found`. Until a real status dashboard
 * ships (port the StatusPageClient polling island from marketing/, or stand
 * up the subdomain behind a Traefik IngressRoute + backend), this page links
 * only to surfaces that actually resolve: the live API health check.
 */
export default function StatusPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Status"
        title="System status."
        subtitle={
          <>
            Live API health and incident updates. A full per-service uptime
            dashboard is on the way.
          </>
        }
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <p className="text-base leading-relaxed text-muted-foreground">
            You can check the live health of the iogrid API directly at the
            health endpoint, which reports{" "}
            <code className="rounded bg-muted px-1.5 py-0.5 text-sm">
              {`{"status":"ok"}`}
            </code>{" "}
            when the platform is serving. A full dashboard with SLO budgets,
            incident history, and 90-day per-service uptime is in progress.
          </p>
          <Link
            href="https://api.iogrid.org/healthz"
            className="mt-8 inline-flex items-center gap-2 rounded-md bg-primary px-5 py-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Check live API health
          </Link>
        </div>
      </section>
    </MarketingShell>
  );
}
