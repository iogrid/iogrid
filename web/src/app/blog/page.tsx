import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Blog",
  description:
    "Long-form posts, architecture deep-dives, postmortems. Coming soon — until then, the repo and changelog at github.com/iogrid/iogrid is the most up-to-date record.",
};

export default function BlogPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Blog"
        title="Posts, deep-dives, postmortems."
        subtitle={
          <>
            We&rsquo;ll write the post the way we&rsquo;d want to read it
            &mdash; long-form, with real metrics, real diagrams, and links to
            the underlying source. First post lands once we have something
            worth saying.
          </>
        }
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Coming soon
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Until the first post ships, the repository at{" "}
            <Link
              href="https://github.com/iogrid/iogrid"
              className="text-foreground underline-offset-2 hover:underline"
            >
              github.com/iogrid/iogrid
            </Link>{" "}
            is the most current record of what we are building and how. The
            commit log, the PR descriptions, and the architecture docs in{" "}
            <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
              docs/
            </code>{" "}
            all live there.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            Topics we expect to write about: the cap + calendar + idle
            scheduler, why we chose Rust on the edge and Go in the
            coordinator, how the per-byte audit log actually proves anything,
            and how we resist the temptation to copy Hola&rsquo;s shortcuts.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="https://github.com/iogrid/iogrid"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Browse the repo
            </Link>
            <Link
              href="https://github.com/iogrid/iogrid/commits/main"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read the changelog
            </Link>
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
