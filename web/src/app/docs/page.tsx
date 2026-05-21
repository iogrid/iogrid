import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Docs",
  description: "Customer SDKs in TypeScript, Python, Go, and Java. REST + gRPC reference. Operator runbooks for self-hosted deployments.",
};

export default function DocsPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Docs"
        title="API reference, SDK guides, runbooks."
        subtitle="Customer SDKs in TypeScript, Python, Go, and Java. REST + gRPC reference. Operator runbooks for self-hosted deployments."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Coming soon. The protobuf schemas at github.com/iogrid/iogrid/tree/main/proto are the source of truth until the docs site ships.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="https://github.com/iogrid/iogrid/tree/main/proto"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Read the protos
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
