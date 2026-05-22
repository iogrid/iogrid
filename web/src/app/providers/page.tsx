import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "For providers",
  description: "One install on a Mac or PC. Cash via Stripe, free unlimited VPN, or charity payouts. Per-byte transparency. Block any category, customer, or destination.",
};

export default function ProvidersPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Providers"
        title="Earn from idle hardware, pick how you get paid."
        subtitle="One install on a Mac or PC. Cash via Stripe, free unlimited VPN, or charity payouts. Per-byte transparency. Block any category, customer, or destination."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Mobile devices are consume-only (platform policy). Desktops, laptops, and homelab boxes are first-class providers.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="/install"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Install the daemon
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
