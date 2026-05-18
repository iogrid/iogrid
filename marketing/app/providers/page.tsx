import type { Metadata } from "next";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";
import { InstallButtons } from "@/components/InstallButtons";
import { TransparencyDemoEmbed } from "@/components/TransparencyDemoEmbed";

export const metadata: Metadata = {
  title: "Earn with iogrid — put your idle hardware to work",
  description:
    "Share bandwidth, compute, GPU, or Mac CI time. See every byte in real time. Get paid in cash, free VPN, $GRID, or charity match.",
};

export default function ProvidersPage() {
  return (
    <>
      <Hero
        eyebrow="For providers"
        title="Your hardware is yours. The receipts are too."
        subtitle={
          <>
            iogrid pays you for the work your idle PC or Mac contributes. Every
            byte that transits your IP is labeled in your dashboard, in real
            time. Block any category at any time. No black boxes.
          </>
        }
        primaryCta={{ href: "#install", label: "Install in 2 minutes" }}
        secondaryCta={{ href: "#how-much", label: "How much can I earn?" }}
        rightSlot={<TransparencyDemoEmbed />}
      />

      <FeatureGrid
        title="Why provide for iogrid"
        features={[
          {
            title: "Four income streams, one daemon",
            body: "Same 3 MB daemon does bandwidth + Docker + GPU + iOS builds. A single Mac with 30 GB / month bandwidth and 4 idle hours of Xcode CI / day = ~$150 / month effective value.",
          },
          {
            title: "Live audit dashboard",
            body: "See every byte categorized: e-commerce, SEO, ad-verification, AI training. Block any category with one click. Block any customer. Block any destination.",
          },
          {
            title: "Choose your currency",
            body: "Cash via USDC + bank off-ramp. Free VPN minutes. $GRID with vesting + price upside. Charity match (we add 25% to any donation). Pick at signup, switch any time.",
          },
          {
            title: "Schedule everything",
            body: "Bandwidth cap. CPU cap. RAM cap. Active-hours calendar. Idle-only mode (default). The daemon only runs when you want it to.",
          },
          {
            title: "Pre-flight protection",
            body: "Anti-abuse filters (CSAM hashes, phishing list, fraud heuristics) run on your machine before traffic relays. You can audit the filter rules locally.",
          },
          {
            title: "No surprises",
            body: "If a customer&rsquo;s use case ever drifts outside your opt-ins, the daemon refuses. We&rsquo;d rather lose a customer than lose your trust.",
          },
        ]}
      />

      <section id="how-much" className="container-page py-16">
        <div className="mx-auto max-w-4xl">
          <h2 className="h-section text-center text-neutral-900">
            Realistic earnings (monthly)
          </h2>
          <p className="mt-4 text-center text-lead">
            Phase 1 figures. Phase 2 forecast roughly 2× as the network scales.
          </p>
          <div className="mt-12 overflow-x-auto rounded-xl border border-neutral-200 bg-white">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-neutral-200 bg-neutral-50">
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Hardware
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Workloads
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Cash equiv.
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Effective value
                  </th>
                </tr>
              </thead>
              <tbody className="font-tabular">
                <tr>
                  <td className="px-4 py-3">Old laptop, US</td>
                  <td className="px-4 py-3 text-sm font-normal">30 GB bandwidth</td>
                  <td className="px-4 py-3">$9</td>
                  <td className="px-4 py-3">$9</td>
                </tr>
                <tr className="bg-neutral-50/60">
                  <td className="px-4 py-3">Linux gaming PC</td>
                  <td className="px-4 py-3 text-sm font-normal">Bandwidth + GPU (4090, 6 hr idle)</td>
                  <td className="px-4 py-3">$45</td>
                  <td className="px-4 py-3">$45</td>
                </tr>
                <tr>
                  <td className="px-4 py-3">M3 MacBook, work-from-home</td>
                  <td className="px-4 py-3 text-sm font-normal">Bandwidth + Docker + iOS CI (4 hr / day)</td>
                  <td className="px-4 py-3">$120</td>
                  <td className="px-4 py-3">$145</td>
                </tr>
                <tr className="bg-neutral-50/60 font-semibold">
                  <td className="px-4 py-3">M3 Mac Studio, 24/7</td>
                  <td className="px-4 py-3 text-sm font-normal">All workloads, always available</td>
                  <td className="px-4 py-3">$210</td>
                  <td className="px-4 py-3">$260</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p className="mt-6 text-center text-xs text-neutral-500">
            &ldquo;Effective value&rdquo; includes the value of free VPN minutes
            for providers who opt into that payout currency.
          </p>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">
            What might transit your IP, and how you control it
          </h2>
          <p className="mt-4 text-neutral-700">
            By default, we route only the workload categories you&rsquo;ve
            opted into. You can flip them on or off at any time. Even within
            an allowed category, every request is logged with the customer name,
            destination, and byte count.
          </p>
          <ul className="mt-6 space-y-2 text-neutral-700">
            <li>
              <strong className="text-neutral-900">Enabled by default:</strong>{" "}
              e-commerce price monitoring, SEO rank checking,
              ad-verification, AI training data collection (public web only),
              iogrid-internal traffic.
            </li>
            <li>
              <strong className="text-neutral-900">Opt-in only:</strong>{" "}
              lead-generation scraping (LinkedIn, Indeed), social-media
              intelligence (Twitter / IG / TikTok), adult-content scraping.
            </li>
            <li>
              <strong className="text-neutral-900">Always blocked, network-wide:</strong>{" "}
              CSAM, phishing, fraud, sanctions-list destinations, anything our
              anti-abuse filter flags. You see attempts in your dashboard so
              you know we&rsquo;re catching them.
            </li>
            <li>
              <strong className="text-neutral-900">Your blocklist:</strong>{" "}
              add your employer&rsquo;s domain, your bank&rsquo;s domain, or
              anything else you don&rsquo;t want associated with your IP.
            </li>
          </ul>
        </div>
      </section>

      <section id="install" className="container-page py-16">
        <div className="mx-auto max-w-3xl text-center">
          <h2 className="h-section text-neutral-900">Install</h2>
          <p className="mt-4 text-lead">
            Signed installer for every desktop OS. Daemon registers as a
            login-item / user service. No root access required on Linux.
          </p>
        </div>
        <div className="mx-auto mt-10 max-w-2xl">
          <InstallButtons />
        </div>
      </section>
    </>
  );
}
