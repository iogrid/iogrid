import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title:
    "Pricing — bandwidth, compute, GPU, iOS builds at 30-60% under market",
  description:
    "Customer pricing: $0.40/GB proxy (vs Bright Data $4-$8), $0.018/vCPU-hour Docker, $0.20/GPU-hour, $0.04/Xcode-minute iOS CI (vs GitHub $0.08). USD, USDC, or $GRID.",
};

/**
 * Customer pricing — folded from marketing/app/pricing/page.tsx into
 * web/'s design system during EPIC #422 Phase 3. The marketing
 * PricingTable component was custom-built for marketing's tokens;
 * here we render the tiers inline as a 4-column grid matching the
 * apex landing's Pillars rhythm.
 */
interface PricingTier {
  id: string;
  name: string;
  price: string;
  unit: string;
  description: string;
  features: string[];
  ctaHref: string;
  ctaLabel: string;
  highlight?: boolean;
}

const TIERS: PricingTier[] = [
  {
    id: "proxy",
    name: "Bandwidth proxy",
    price: "$0.40",
    unit: "per GB",
    description:
      "Residential IPs with cryptographic audit. 95% pool average; geo-targeting on every request.",
    features: [
      "Residential IPs across 195+ countries",
      "Per-byte category labels in audit log",
      "Session stickiness up to 30 minutes",
      "Geo-targeted at country and city level",
      "SOCKS5 + HTTP CONNECT",
      "Volume discounts above 500 GB / month",
    ],
    ctaHref: "/customer",
    ctaLabel: "Start with proxy",
  },
  {
    id: "ios-build",
    name: "iOS build CI",
    price: "$0.04",
    unit: "per minute",
    description:
      "Pay-per-minute Mac CI. No 24-hour leases. No idle waste. Bring your Xcode project.",
    features: [
      "Ephemeral macOS VMs via Tart",
      "Latest 3 Xcode versions; older on request",
      "Apple Silicon (M1, M2, M3) providers",
      "S3 artifact bucket included",
      "GitHub Actions runner image available",
      "No minimum spend",
    ],
    ctaHref: "/customer",
    ctaLabel: "Run a build",
    highlight: true,
  },
  {
    id: "compute",
    name: "Docker compute",
    price: "$0.018",
    unit: "per vCPU-hour",
    description:
      "Linux Docker workloads on idle home + Mac hardware. gVisor-isolated. Cheaper than spot.",
    features: [
      "Any OCI image",
      "x86_64 and ARM64 providers",
      "gVisor or Kata Container isolation",
      "Up to 16 GB RAM per container",
      "Bandwidth included up to 50 GB / job",
      "Bring-your-registry credentials",
    ],
    ctaHref: "/customer",
    ctaLabel: "Submit a container",
  },
  {
    id: "gpu",
    name: "GPU inference",
    price: "$0.20",
    unit: "per GPU-hour",
    description:
      "Consumer GPUs (4090, 5090, Apple Silicon MLX). For batch inference and fine-tuning.",
    features: [
      "NVIDIA consumer cards (24 GB+ VRAM)",
      "Apple Silicon MLX (M3 Max, M4)",
      "Per-second billing after first minute",
      "Hugging Face TGI / vLLM templates",
      "Bring your own model weights",
      "Pre-flight benchmark before charge",
    ],
    ctaHref: "/customer",
    ctaLabel: "Run inference",
  },
];

const VOLUME = [
  { spend: "$0 - $500", discount: "List price", includes: "Standard support, audit log" },
  { spend: "$500 - $5,000", discount: "10% off", includes: "Priority email, custom geo-targeting" },
  { spend: "$5,000 - $50,000", discount: "20% off", includes: "Named contact, SLA, slack channel" },
  { spend: "$50,000+", discount: "Custom", includes: "Dedicated pool, custom contract" },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "Is there a free tier?",
    a: "Every new account gets $5 in credit. No card required to start; we ask for one when you exceed it.",
  },
  {
    q: "Do I need to KYC?",
    a: "Fiat payments through Stripe trigger Stripe's standard AML at high volume. On-chain USDC over $10K / month triggers Sumsub. $GRID-only customers face per-wallet limits.",
  },
  {
    q: "What happens if a provider drops mid-job?",
    a: "Jobs are restartable; the gateway re-dispatches to another provider within seconds. You're only charged for completed seconds.",
  },
  {
    q: "How does the $GRID 20% discount work?",
    a: "Pay invoices in $GRID rather than USDC, and the gateway applies a 20% discount. The $GRID flows through to providers + the burn wallet.",
  },
];

export default function PricingPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Pricing"
        title="Pay per byte, minute, or hour."
        subtitle={
          <>
            No annual contracts. No SDR calls. The same per-unit price applies
            to a $5 spend and a $5,000 spend, with volume discounts kicking in
            on the way up. Pay with USD, USDC, or $GRID.
          </>
        }
      />

      <section id="tiers" className="border-b border-border">
        <div className="mx-auto max-w-6xl px-6 py-16">
          <div className="grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-2 lg:grid-cols-4">
            {TIERS.map((t) => (
              <TierCard key={t.id} tier={t} />
            ))}
          </div>
          <p className="mt-6 text-xs text-muted-foreground">
            All prices in USD. USDC payments are list price. $GRID payments
            are 20% off. Stripe Tax handles your local VAT.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Volume discounts
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Monthly spend
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Discount
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Includes
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-background">
                {VOLUME.map((row) => (
                  <tr key={row.spend}>
                    <td className="px-4 py-3 text-foreground">{row.spend}</td>
                    <td className="px-4 py-3 text-foreground">{row.discount}</td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.includes}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
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
                <dd className="mt-2 text-base leading-relaxed text-muted-foreground">
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

function TierCard({ tier }: { tier: PricingTier }) {
  return (
    <div
      className={`flex flex-col gap-4 bg-background p-8 ${tier.highlight ? "ring-1 ring-inset ring-primary" : ""}`}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {tier.name}
        </span>
        {tier.highlight ? (
          <span className="rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
            Differentiator
          </span>
        ) : null}
      </div>
      <div className="flex items-baseline gap-2">
        <span className="text-3xl font-semibold tracking-tight text-foreground">
          {tier.price}
        </span>
        <span className="text-sm text-muted-foreground">{tier.unit}</span>
      </div>
      <p className="text-sm leading-relaxed text-muted-foreground">
        {tier.description}
      </p>
      <ul className="mt-2 space-y-2 text-sm text-muted-foreground">
        {tier.features.map((f) => (
          <li key={f} className="flex items-start gap-2">
            <span aria-hidden className="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-foreground/40" />
            <span>{f}</span>
          </li>
        ))}
      </ul>
      <Link
        href={tier.ctaHref}
        className="mt-auto inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:border-foreground/40 hover:bg-muted"
      >
        {tier.ctaLabel}
      </Link>
    </div>
  );
}
