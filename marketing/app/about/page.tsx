import type { Metadata } from "next";
import { Hero } from "@/components/Hero";

export const metadata: Metadata = {
  title: "About",
  description:
    "iogrid is a transparent mesh network. Our mission is to make participation in distributed infrastructure honest, fair, and well-paid.",
};

export default function AboutPage() {
  return (
    <>
      <Hero
        eyebrow="About"
        title="A network worth participating in."
        subtitle={
          <>
            iogrid is a mesh network where you can rent out the idle capacity
            of your PC or Mac — bandwidth, CPU, GPU, or a few hours of Xcode CI.
            We exist because the existing players in this market have spent a
            decade hiding what they do with their users&rsquo; hardware. We&rsquo;re
            building the opposite.
          </>
        }
      />

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">Principles</h2>
          <ol className="mt-6 space-y-6 text-neutral-700">
            <li>
              <strong className="text-neutral-900">1. Transparency is a feature, not a slogan.</strong>{" "}
              Every byte that transits a provider&rsquo;s IP is labeled by
              category, customer, and destination. Providers can block any of
              those three at any time. The audit log is cryptographically
              signed and replicated to customer invoices.
            </li>
            <li>
              <strong className="text-neutral-900">2. Provider consent is granular.</strong>{" "}
              Coarse opt-ins like &ldquo;allow proxy traffic&rdquo; aren&rsquo;t
              enough. Providers choose which workload categories are eligible
              for their hardware — and switch them on or off without
              uninstalling anything.
            </li>
            <li>
              <strong className="text-neutral-900">3. Multi-currency payouts.</strong>{" "}
              Cash via USDC + bank off-ramp, free VPN minutes, $GRID tokens
              with optional long-term vesting, or charity match. Pick the one
              that maps to your goals. We never force providers into a
              currency we control.
            </li>
            <li>
              <strong className="text-neutral-900">4. Anti-abuse is upstream.</strong>{" "}
              CSAM, phishing, fraud, sanctions-list traffic — blocked before
              bytes leave the gateway. The same filter rules run on the
              provider&rsquo;s daemon for auditability. We&rsquo;d rather lose
              a customer than route their abuse.
            </li>
            <li>
              <strong className="text-neutral-900">5. Power asymmetry favors the supplier.</strong>{" "}
              Almost every network in this category is structured to benefit
              its customers over its providers. We invert that. Providers can
              kick customers off their hardware. Customers can&rsquo;t kick
              providers out of the network.
            </li>
            <li>
              <strong className="text-neutral-900">6. Open source where it matters.</strong>{" "}
              The provider daemon is AGPL-licensed in Phase 1, so anyone can
              verify what their hardware is doing. The audit verifier and
              category classifier are open-source too. The coordinator
              microservices stay source-available for operational reasons.
            </li>
          </ol>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">What we do not do</h2>
          <ul className="mt-6 space-y-3 text-neutral-700">
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
              We do not promise $GRID will appreciate. It might. It might not.
              Read the <a href="/token" className="text-primary-600 underline">$GRID page</a> for the honest version.
            </li>
            <li>
              We do not sell, share, or rent customer audit logs to third parties.
              They&rsquo;re generated for the customer, the involved provider,
              and our anti-abuse system. That&rsquo;s it.
            </li>
          </ul>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">Contact</h2>
          <p className="mt-4 text-neutral-700">
            Press, partnerships, abuse reports, and legal:{" "}
            <a href="mailto:hello@iogrid.org" className="text-primary-600 underline">
              hello@iogrid.org
            </a>
            . Provider and customer support: in-product, or{" "}
            <a href="mailto:support@iogrid.org" className="text-primary-600 underline">
              support@iogrid.org
            </a>
            . Security disclosures:{" "}
            <a href="mailto:security@iogrid.org" className="text-primary-600 underline">
              security@iogrid.org
            </a>
            .
          </p>
        </div>
      </section>
    </>
  );
}
