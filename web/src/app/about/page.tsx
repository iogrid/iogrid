import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "About",
  description:
    "iogrid is a transparent mesh network. Our mission is to make participation in distributed infrastructure honest, fair, and well-paid.",
};

/**
 * About page — folded from marketing/app/about/page.tsx into web/'s
 * design system during EPIC #422 Phase 3. Content preserved verbatim;
 * markup retargeted to shadcn-style tokens (text-foreground /
 * text-muted-foreground / border-border / bg-background).
 */
export default function AboutPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="About"
        title="A network worth participating in."
        subtitle={
          <>
            iogrid is a mesh network where you can rent out the idle capacity of
            your PC or Mac &mdash; bandwidth, CPU, GPU, or a few hours of Xcode
            CI. We exist because the existing players in this market have spent
            a decade hiding what they do with their users&rsquo; hardware.
            We&rsquo;re building the opposite.
          </>
        }
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Principles
          </h2>
          <ol className="mt-8 space-y-6 text-base leading-relaxed text-muted-foreground">
            <li>
              <strong className="text-foreground">
                1. Transparency is a feature, not a slogan.
              </strong>{" "}
              Every byte that transits a provider&rsquo;s IP is labeled by
              category, customer, and destination. Providers can block any of
              those three at any time. The audit log is cryptographically
              signed and replicated to customer invoices.
            </li>
            <li>
              <strong className="text-foreground">
                2. Provider consent is granular.
              </strong>{" "}
              Coarse opt-ins like &ldquo;allow proxy traffic&rdquo; aren&rsquo;t
              enough. Providers choose which workload categories are eligible
              for their hardware &mdash; and switch them on or off without
              uninstalling anything.
            </li>
            <li>
              <strong className="text-foreground">
                3. Multi-currency payouts.
              </strong>{" "}
              Cash via USDC + bank off-ramp, free VPN minutes, $GRID tokens
              with optional long-term vesting, or charity match. Pick the one
              that maps to your goals. We never force providers into a currency
              we control.
            </li>
            <li>
              <strong className="text-foreground">
                4. Anti-abuse is upstream.
              </strong>{" "}
              CSAM, phishing, fraud, sanctions-list traffic &mdash; blocked
              before bytes leave the gateway. The same filter rules run on the
              provider&rsquo;s daemon for auditability. We&rsquo;d rather lose a
              customer than route their abuse.
            </li>
            <li>
              <strong className="text-foreground">
                5. Power asymmetry favors the supplier.
              </strong>{" "}
              Almost every network in this category is structured to benefit
              its customers over its providers. We invert that. Providers can
              kick customers off their hardware. Customers can&rsquo;t kick
              providers out of the network.
            </li>
            <li>
              <strong className="text-foreground">
                6. Open source where it matters.
              </strong>{" "}
              The provider daemon is AGPL-licensed in Phase 1, so anyone can
              verify what their hardware is doing. The audit verifier and
              category classifier are open-source too. The coordinator
              microservices stay source-available for operational reasons.
            </li>
          </ol>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What we do not do
          </h2>
          <ul className="mt-8 space-y-4 text-base leading-relaxed text-muted-foreground">
            <li>
              We do not sell residential bandwidth to defense contractors,
              influence-operations consultancies, or ad-tech firms whose
              business model is profiling.
            </li>
            <li>
              We do not promise providers a fixed income. Earnings depend on
              customer demand, geographic distribution, and the workload mix
              the provider opts into.
            </li>
            <li>
              We do not sell, share, or rent customer audit logs to third
              parties. They&rsquo;re generated for the customer, the involved
              provider, and our anti-abuse system. That&rsquo;s it.
            </li>
          </ul>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Contact
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Press, partnerships, abuse reports, and legal:{" "}
            <Link
              href="mailto:hello@iogrid.org"
              className="text-foreground underline-offset-2 hover:underline"
            >
              hello@iogrid.org
            </Link>
            . Provider and customer support: in-product, or{" "}
            <Link
              href="mailto:support@iogrid.org"
              className="text-foreground underline-offset-2 hover:underline"
            >
              support@iogrid.org
            </Link>
            . Security disclosures:{" "}
            <Link
              href="mailto:security@iogrid.org"
              className="text-foreground underline-offset-2 hover:underline"
            >
              security@iogrid.org
            </Link>
            .
          </p>
        </div>
      </section>
    </MarketingShell>
  );
}
