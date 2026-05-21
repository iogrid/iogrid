import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Bandwidth proxy",
  description: "SOCKS5-over-TLS to providers who consented to bandwidth-share. Per-byte audit log, per-customer billing, per-provider opt-out.",
};

export default function ProxyPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Proxy"
        title="Residential IP proxy with full transparency."
        subtitle="SOCKS5-over-TLS to providers who consented to bandwidth-share. Per-byte audit log, per-customer billing, per-provider opt-out."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Compatible with every HTTP client that speaks SOCKS5h. No CAPTCHAs against your residential IPs.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Get a proxy key
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
