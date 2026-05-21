import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "$GRID token",
  description: "A deflationary work-token on Solana. Earned by providers, paid by customers for a 20% discount, burnable for VPN bandwidth.",
};

export default function TokenPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Token"
        title="$GRID — earn it, spend it, or burn it for VPN."
        subtitle="A deflationary work-token on Solana. Earned by providers, paid by customers for a 20% discount, burnable for VPN bandwidth."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Token economics: 1B cap, halving every 2 years, 4-year LP lockup, multi-sig treasury. Full breakdown in the whitepaper.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="/legal"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Read the whitepaper
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
