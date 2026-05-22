import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Docs",
  description:
    "Customer SDKs in TypeScript, Python, Go, and Java. Protobuf schemas as the source of truth. Operator runbooks for self-hosted deployments. Coming soon.",
};

export default function DocsPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Docs"
        title="SDKs, protobuf reference, operator runbooks."
        subtitle="Customer SDKs in TypeScript, Python, Go, and Java. REST + gRPC reference generated from the protos. Operator runbooks for self-hosted deployments."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Coming soon
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            The proper docs site is in progress. Until it lands, the canonical
            sources are:
          </p>
          <ul className="mt-6 space-y-4 text-base leading-relaxed text-muted-foreground">
            <li>
              <span className="font-semibold text-foreground">Protobuf schemas:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/tree/main/proto"
                className="text-foreground underline-offset-2 hover:underline"
              >
                github.com/iogrid/iogrid/tree/main/proto
              </Link>{" "}
              &mdash; the source of truth for the gRPC + REST surface.
            </li>
            <li>
              <span className="font-semibold text-foreground">TypeScript SDK:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/tree/main/sdks/typescript"
                className="text-foreground underline-offset-2 hover:underline"
              >
                @iogrid/sdk on GitHub
              </Link>{" "}
              &mdash; README has end-to-end examples for every workload type.
            </li>
            <li>
              <span className="font-semibold text-foreground">Python SDK:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/tree/main/sdks/python"
                className="text-foreground underline-offset-2 hover:underline"
              >
                iogrid on PyPI
              </Link>{" "}
              &mdash; async-first, httpx-based, Python 3.10+.
            </li>
            <li>
              <span className="font-semibold text-foreground">Go SDK:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/tree/main/sdks/go"
                className="text-foreground underline-offset-2 hover:underline"
              >
                go.iogrid.org/sdk
              </Link>{" "}
              &mdash; idiomatic Go client, context-aware.
            </li>
            <li>
              <span className="font-semibold text-foreground">Java SDK:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/tree/main/sdks/java"
                className="text-foreground underline-offset-2 hover:underline"
              >
                org.iogrid:sdk on Maven Central
              </Link>{" "}
              &mdash; Java 17+, GraalVM-friendly.
            </li>
            <li>
              <span className="font-semibold text-foreground">Architecture:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/blob/main/docs/ARCHITECTURE.md"
                className="text-foreground underline-offset-2 hover:underline"
              >
                docs/ARCHITECTURE.md
              </Link>{" "}
              &mdash; how the daemon, coordinator, and management plane fit
              together.
            </li>
            <li>
              <span className="font-semibold text-foreground">Operator runbooks:</span>{" "}
              <Link
                href="https://github.com/iogrid/iogrid/blob/main/docs/RUNBOOKS.md"
                className="text-foreground underline-offset-2 hover:underline"
              >
                docs/RUNBOOKS.md
              </Link>{" "}
              &mdash; provisioning, chart bump, failover recovery.
            </li>
          </ul>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="https://github.com/iogrid/iogrid"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Browse the repo
            </Link>
            <Link
              href="https://github.com/iogrid/iogrid/tree/main/proto"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read the protos
            </Link>
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
