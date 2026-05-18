import type { Metadata } from "next";
import Link from "next/link";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";
import { TransparencyDemoEmbed } from "@/components/TransparencyDemoEmbed";

export const metadata: Metadata = {
  title: "Bandwidth proxy — residential IPs with cryptographic audit",
  description:
    "Residential proxy at $0.40 per GB with per-byte category labels in your audit log. Geo-targeted, session-sticky, 195+ countries.",
};

export default function ProxyPage() {
  return (
    <>
      <Hero
        eyebrow="Bandwidth proxy"
        title="Residential IPs. Cryptographic audit."
        subtitle={
          <>
            Same coverage as Bright Data. Roughly a third of the price. Plus a
            live audit log of every byte categorized by purpose — so when your
            scraper hits a wall, you have receipts.
          </>
        }
        primaryCta={{ href: "/pricing", label: "Start at $0.40 / GB" }}
        secondaryCta={{ href: "#features", label: "How it works" }}
        rightSlot={<TransparencyDemoEmbed />}
      />

      <FeatureGrid
        title="What you get"
        features={[
          {
            title: "Per-byte audit log",
            body: "Every request is labeled by category (e-commerce, SEO, ad-verification, etc.). Export to CSV or query via API.",
          },
          {
            title: "Geo-targeting",
            body: "Country, region, city. ASN-targeting for advanced use cases. Session-stickiness up to 30 minutes.",
          },
          {
            title: "SOCKS5 + HTTP CONNECT",
            body: "Drop-in compatible with anything that speaks proxy. No SDK required. TLS termination at the gateway.",
          },
          {
            title: "Pre-flight filtering",
            body: "CSAM hashes, phishing list, fraud heuristics run server-side and on provider hardware. We block before bytes leave.",
          },
          {
            title: "Provider consent",
            body: "Providers see exactly what runs through their IP and block any category. Cleaner pool, better acceptance rates.",
          },
          {
            title: "Volume discounts",
            body: "Above 500 GB / month, prices drop in tiers. Above 5 TB, pricing is custom and you have a named contact.",
          },
        ]}
      />

      <section id="features" className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">How a request flows</h2>
          <ol className="mt-6 space-y-4 text-neutral-700">
            <li>
              <strong className="text-neutral-900">1. SDK or proxy URL.</strong>{" "}
              Point your scraper at <code className="font-mono">proxy.iogrid.org:443</code> with
              your API key and target geography.
            </li>
            <li>
              <strong className="text-neutral-900">2. Pre-flight filter.</strong>{" "}
              The gateway runs the destination through our anti-abuse filter.
              Disallowed targets are rejected before any provider IP is touched.
            </li>
            <li>
              <strong className="text-neutral-900">3. Routing.</strong> The
              workload is dispatched to a provider whose opt-ins match this
              category, in the requested geography, with capacity available.
            </li>
            <li>
              <strong className="text-neutral-900">4. Relay.</strong> The
              provider daemon (Rust, 3 MB RAM) forwards traffic via WireGuard.
              Your HTTPS payload is never decrypted by the daemon or the
              coordinator.
            </li>
            <li>
              <strong className="text-neutral-900">5. Audit.</strong> Bytes are
              categorized at the gateway, signed, and written to both your
              customer audit log and the provider&rsquo;s transparency feed.
            </li>
          </ol>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="rounded-2xl border border-neutral-200 bg-neutral-50 p-8 text-center md:p-12">
          <h2 className="h-section text-neutral-900">
            Ready to switch from Bright Data?
          </h2>
          <p className="mx-auto mt-4 max-w-2xl text-lead">
            We&rsquo;ll mirror your current setup, run a side-by-side for 30
            days, and you keep whichever is faster + cheaper. Most teams pick
            us. No commitment, no claw-back.
          </p>
          <Link href="/pricing" className="btn-primary mt-8">
            See pricing &amp; start
          </Link>
        </div>
      </section>
    </>
  );
}
