import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Transparency",
  description: "Aggregate stats on traffic categories, legal requests received, network growth, and per-region opt-out rates. No PII, no customer identifiers.",
};

export default function TransparencyPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Transparency"
        title="Quarterly public-facing transparency reports."
        subtitle="Aggregate stats on traffic categories, legal requests received, network growth, and per-region opt-out rates. No PII, no customer identifiers."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            First report ships at end of 2026-Q2. Until then, see the public TRACKER ledger and the docs/transparency/ subdir in the repo.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="https://github.com/iogrid/iogrid/tree/main/docs/transparency"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              View transparency drafts
            </Link>
            <Link
              href="/pricing"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              See pricing
            </Link>
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
