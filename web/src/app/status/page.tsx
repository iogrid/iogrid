import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Status",
  description:
    "iogrid operational status. Live posture lives at status.iogrid.org.",
};

/**
 * Status landing — folded from marketing/app/status/page.tsx into
 * web/'s design system during EPIC #422 Phase 3.
 *
 * The full live status dashboard (60-second polling against
 * telemetry-svc /status/posture) lives at the dedicated
 * status.iogrid.org subdomain — that subdomain has its own static
 * deploy and its own HTTPRoute (`gateways/httproute-status.yaml`),
 * unaffected by Phase 3. This /status route on the apex serves as a
 * SEO-discoverable landing that points users at the live dashboard.
 *
 * If we ever want the live dashboard inline at iogrid.org/status,
 * the StatusPageClient component can be ported from marketing/
 * (was a client-side island polling /status/posture).
 */
export default function StatusPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Status"
        title="System status."
        subtitle={
          <>
            Live operational posture, SLO budgets, incident history, and 90-day
            uptime per service. Updated every 60 seconds from telemetry-svc.
          </>
        }
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <p className="text-base leading-relaxed text-muted-foreground">
            The live status dashboard is hosted at{" "}
            <Link
              href="https://status.iogrid.org"
              className="text-foreground underline-offset-2 hover:underline"
            >
              status.iogrid.org
            </Link>
            . It is intentionally served from a dedicated subdomain so the
            status page stays reachable when the app itself is degraded.
          </p>
          <Link
            href="https://status.iogrid.org"
            className="mt-8 inline-flex items-center gap-2 rounded-md bg-primary px-5 py-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Go to status dashboard
          </Link>
        </div>
      </section>
    </MarketingShell>
  );
}
