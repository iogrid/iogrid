import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Blog",
  description: "We write the post the way we%DESCRIPTION%rsquo;d want to read it — long-form, with real metrics, real diagrams, and links to the underlying source.",
};

export default function BlogPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Blog"
        title="Posts, deep-dives, postmortems."
        subtitle="We write the post the way we%SUBTITLE%rsquo;d want to read it — long-form, with real metrics, real diagrams, and links to the underlying source."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Coming soon. The repo and changelog at github.com/iogrid/iogrid is the most up-to-date record until the first post lands.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="https://github.com/iogrid/iogrid"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Browse the repo
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
