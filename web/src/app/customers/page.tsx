import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "For customers — residential proxy, GPU, iOS builds at 50% off",
  description:
    "Buy residential SOCKS5 proxy by the byte, GPU inference by the second, or macOS-native iOS builds at half GitHub Actions pricing. Per-byte transparency. No long-term contracts.",
};

// /customers marketing page (Refs #460).
// Sister surface to /providers (which targets people sharing their PC).
// This targets the enterprise buyers — proxy, compute, GPU, iOS-build.
// The shape mirrors /providers (Hero + 3 use-case sections + Get-started
// + FAQ) so the marketing footprint is consistent.

const USE_CASES = [
  {
    n: "1",
    title: "Residential proxy by the byte",
    body: "SOCKS5 endpoints rotated across thousands of residential IPs in real consumer networks (Mac, PC, mobile). Bot detectors cannot tell our traffic apart from a real subscriber. Pay only for the bytes you ship — no monthly commit, no per-port rental.",
  },
  {
    n: "2",
    title: "Docker compute + GPU inference",
    body: "Run any OCI-compliant container on our provider mesh. CPU-only jobs run on idle home PCs; GPU jobs route to RTX 30/40/50-class cards. Per-second billing, sandboxed namespaces, egress allow-listed per workload. ~30-50% of Lambda / Fly equivalents.",
  },
  {
    n: "3",
    title: "iOS builds at ~50% of GitHub Actions",
    body: "macOS-native build agents on real Apple Silicon hardware. Xcode 16, Tart-isolated per-build VMs, full Xcode toolchain pre-warmed. CI workflows that take 12 minutes on hosted GitHub runners finish in 5-8 minutes here, at roughly half the per-minute price.",
  },
];

const STEPS = [
  {
    title: "Mint an API key",
    body: "Sign in, name your workspace, mint a key. Free \$5 of credit included to walk the docs end-to-end. No card required for first 1 GB / first 60 GPU-seconds.",
  },
  {
    title: "Wire one of four SDKs",
    body: "TypeScript, Python, Go, or Java. Each SDK speaks Connect-RPC; the same proto contract works from curl too. Self-serve docs include a 10-line proxy quickstart and a working vCard-enrich example.",
  },
  {
    title: "Pay only for what you use",
    body: "Stripe handles cards; \$GRID on-chain handles crypto. Per-byte / per-second metering. Daily invoice, monthly aggregated receipt. Cancel any time — no annual contracts, no minimums.",
  },
];

const FAQ = [
  {
    q: "How is this not Bright Data / Oxylabs?",
    a: "Three differences. (1) Per-byte transparency — every provider sees the bytes they relayed, you see the bytes you spent, the audit trail is on-chain. (2) Open SDKs, no salesperson — mint a key and start in 60 seconds. (3) Workloads beyond proxy: GPU, iOS-build, generic Docker — the same provider mesh, the same pricing model.",
  },
  {
    q: "What about the abuse layer?",
    a: "Default-deny on top-1000 sensitive categories (NCMEC PhotoDNA, Google Safe Browsing, PhishTank). Customers can block per-destination, per-category, per-country at the API layer. Every block is logged + auditable. We do NOT relay traffic we cannot prove is legal.",
  },
  {
    q: "Will my workload starve when providers go offline?",
    a: "Workloads are scheduled across many providers in parallel; a single provider going offline triggers a sub-second re-route. Replicas-per-job are bid up automatically until your SLO is met. Bandwidth jobs replicate the request across N parents and reconcile responses; deterministic compute jobs use checkpoint-and-resume.",
  },
  {
    q: "Pricing in one line?",
    a: "\$0.50/GB for residential SOCKS5, \$0.0004/CPU-second, \$0.0012/GPU-second on RTX 4090-class, \$0.08/macOS-build-minute. First \$5 free.",
  },
];

export default function CustomersPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="For customers"
        title="Residential proxy, GPU, iOS builds — by the byte, by the second, by the minute."
        body="One API key. Four SDKs. Pay only for what you ship. \$5 free credit to walk the docs end-to-end."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What you can buy
          </h2>
          <div className="mt-8 grid gap-8 md:grid-cols-3">
            {USE_CASES.map((c) => (
              <div key={c.title} className="space-y-3">
                <div className="text-3xl font-light text-muted-foreground">
                  {c.n}
                </div>
                <h3 className="text-base font-semibold text-foreground">
                  {c.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {c.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            How to start
          </h2>
          <dl className="mt-8 space-y-6">
            {STEPS.map((s) => (
              <div key={s.title}>
                <dt className="text-base font-semibold text-foreground">
                  {s.title}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {s.body}
                </dd>
              </div>
            ))}
          </dl>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="/account"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Mint an API key
            </Link>
            <Link
              href="/docs"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read the docs
            </Link>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            FAQ
          </h2>
          <dl className="mt-8 space-y-8">
            {FAQ.map((row) => (
              <div key={row.q}>
                <dt className="text-base font-semibold text-foreground">
                  {row.q}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {row.a}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </MarketingShell>
  );
}
