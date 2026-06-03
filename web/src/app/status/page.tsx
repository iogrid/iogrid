import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";
import { StatusPageClient } from "./status-page-client";

export const metadata: Metadata = {
  title: "Status",
  description:
    "iogrid operational status — live API health check and incident updates.",
};

/**
 * Status landing — folded from marketing/app/status/page.tsx into
 * web/'s design system during EPIC #422 Phase 3.
 *
 * #674 (2026-06-04): the real dashboard. The StatusPageClient island
 * polls /status/feed (same-origin route handler → gateway-bff public
 * /status/posture proxy → telemetry-svc's posture generator) and
 * renders overall posture, per-service SLO budgets, and incidents.
 * The previous version of this page only linked the raw healthz
 * endpoint (see #668 for why the status.iogrid.org subdomain was
 * abandoned in favour of this apex page).
 */
export default function StatusPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Status"
        title="System status."
        subtitle={
          <>
            Live per-service posture, SLO budgets, and incident history —
            straight from the telemetry plane.
          </>
        }
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <StatusPageClient />
          <p className="mt-10 text-sm leading-relaxed text-muted-foreground">
            Prefer raw signals? The API health endpoint reports{" "}
            <code className="rounded bg-muted px-1.5 py-0.5 text-sm">
              {`{"status":"ok"}`}
            </code>{" "}
            when the platform is serving.
          </p>
          <Link
            href="https://api.iogrid.org/healthz"
            className="mt-4 inline-flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm font-medium transition-colors hover:bg-muted"
          >
            Check live API health
          </Link>
        </div>
      </section>
    </MarketingShell>
  );
}
