import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Docker compute",
  description: "Container workloads on the mesh — Docker / OCI images, scheduled across provider machines by capability, geo, and reputation.",
};

export default function ComputePage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Compute"
        title="Run containers at residential prices."
        subtitle="Container workloads on the mesh — Docker / OCI images, scheduled across provider machines by capability, geo, and reputation."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Pay per CPU-hour, no per-machine commitments. Same SDKs as proxy and GPU workloads.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Start a workload
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
