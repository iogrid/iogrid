import type { Metadata } from "next";
import { Hero } from "@/components/Hero";
import { PricingTable } from "@/components/PricingTable";
import { customerPricing } from "@/content/pricing";

export const metadata: Metadata = {
  title: "Pricing — bandwidth, compute, GPU, iOS builds",
  description:
    "Customer pricing: $0.40 / GB proxy, $0.018 / vCPU-hour Docker, $0.20 / GPU-hour, $0.04 / minute iOS CI. Pay with USD, USDC, or $GRID.",
};

export default function PricingPage() {
  return (
    <>
      <Hero
        eyebrow="Pricing"
        title="Pay per byte, minute, or hour."
        subtitle={
          <>
            No annual contracts. No SDR calls. The same per-unit price applies
            to a $5 spend and a $5,000 spend, with volume discounts kicking in
            on the way up. Pay with USD, USDC, or $GRID.
          </>
        }
        primaryCta={{ href: "#tiers", label: "See the four products" }}
      />

      <div id="tiers">
        <PricingTable
          tiers={customerPricing}
          caption="All prices in USD. USDC payments are list price. $GRID payments are 20% off. Stripe Tax handles your local VAT."
        />
      </div>

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">Volume discounts</h2>
          <div className="mt-6 overflow-x-auto rounded-xl border border-neutral-200 bg-white">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-neutral-200 bg-neutral-50">
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Monthly spend
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Discount
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Includes
                  </th>
                </tr>
              </thead>
              <tbody className="font-tabular">
                <tr>
                  <td className="px-4 py-3">$0 – $500</td>
                  <td className="px-4 py-3">List price</td>
                  <td className="px-4 py-3 text-sm font-normal">Standard support, audit log</td>
                </tr>
                <tr className="bg-neutral-50/60">
                  <td className="px-4 py-3">$500 – $5,000</td>
                  <td className="px-4 py-3">10% off</td>
                  <td className="px-4 py-3 text-sm font-normal">Priority email, custom geo-targeting</td>
                </tr>
                <tr>
                  <td className="px-4 py-3">$5,000 – $50,000</td>
                  <td className="px-4 py-3">20% off</td>
                  <td className="px-4 py-3 text-sm font-normal">Named contact, SLA, slack channel</td>
                </tr>
                <tr className="bg-neutral-50/60">
                  <td className="px-4 py-3">$50,000+</td>
                  <td className="px-4 py-3">Custom</td>
                  <td className="px-4 py-3 text-sm font-normal">Dedicated pool, custom contract</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">FAQ</h2>
          <dl className="mt-6 space-y-6">
            <div>
              <dt className="font-semibold text-neutral-900">
                Is there a free tier?
              </dt>
              <dd className="mt-1 text-neutral-700">
                Every new account gets $5 in credit. No card required to start;
                we ask for one when you exceed it.
              </dd>
            </div>
            <div>
              <dt className="font-semibold text-neutral-900">
                Do I need to KYC?
              </dt>
              <dd className="mt-1 text-neutral-700">
                Fiat payments through Stripe trigger Stripe&rsquo;s standard
                AML at high volume. On-chain USDC over $10K / month triggers
                Sumsub. $GRID-only customers face per-wallet limits.
              </dd>
            </div>
            <div>
              <dt className="font-semibold text-neutral-900">
                What happens if a provider drops mid-job?
              </dt>
              <dd className="mt-1 text-neutral-700">
                Jobs are restartable; the gateway re-dispatches to another
                provider within seconds. You&rsquo;re only charged for completed
                seconds.
              </dd>
            </div>
            <div>
              <dt className="font-semibold text-neutral-900">
                How does the $GRID 20% discount work?
              </dt>
              <dd className="mt-1 text-neutral-700">
                Pay invoices in $GRID rather than USDC, and the gateway applies
                a 20% discount. The $GRID flows through to providers + the burn
                wallet. Read more on the <a href="/token" className="text-primary-600 underline">$GRID page</a>.
              </dd>
            </div>
          </dl>
        </div>
      </section>
    </>
  );
}
