import Link from "next/link";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";
import { TransparencyDemoEmbed } from "@/components/TransparencyDemoEmbed";
import { ComparisonTable } from "@/components/ComparisonTable";
import { Stats } from "@/components/Stats";
import { InstallButtons } from "@/components/InstallButtons";

export default function LandingPage() {
  return (
    <>
      <Hero
        eyebrow="Transparent mesh network"
        title={
          <>
            The mesh that shows you{" "}
            <span className="text-primary-500">every byte</span>.
          </>
        }
        subtitle={
          <>
            Share idle bandwidth, CPU, GPU, or a few hours of Mac time. See exactly
            what your hardware is doing in real time. Get paid in cash, free VPN,
            or $GRID — your choice.
          </>
        }
        primaryCta={{ href: "/providers", label: "Become a provider" }}
        secondaryCta={{ href: "/pricing", label: "Buy services" }}
        rightSlot={<TransparencyDemoEmbed />}
      />

      <Stats />

      <FeatureGrid
        title="Four networks. One mesh."
        subtitle="Same daemon runs all four workloads — and you choose which ones are on."
        features={[
          {
            title: "Bandwidth proxy",
            body: "Residential proxy with live per-byte category labels. 30% cheaper than incumbents.",
          },
          {
            title: "Docker compute",
            body: "Run OCI containers on home Linux + Mac providers. gVisor-isolated.",
          },
          {
            title: "GPU inference",
            body: "Consumer GPUs (4090, 5090, Apple Silicon MLX) for batch inference + fine-tuning.",
          },
          {
            title: "iOS build CI",
            body: "Pay-per-minute Mac CI. Half the price of GitHub Actions Mac. No 24-hour leases.",
          },
        ]}
        columns={4}
      />

      <section className="container-page py-16">
        <div className="rounded-2xl bg-primary-900 px-8 py-16 text-white">
          <div className="grid items-center gap-12 lg:grid-cols-2">
            <div>
              <span className="pill bg-primary-700/50 text-primary-100">
                Anti-Hola moat
              </span>
              <h2 className="h-section mt-4 text-white">
                Radical transparency, not vague consent.
              </h2>
              <p className="mt-4 text-lg text-primary-100">
                Every byte that transits your IP is labeled, in real time, in
                your provider dashboard. Block a category, a customer, or a
                destination with one click. The same audit log is published to
                the coordinator for cryptographic proof — what we tell you
                is what we tell our customers.
              </p>
              <p className="mt-4 text-sm text-primary-200">
                We exist because Hola sold its users&rsquo; bandwidth without
                meaningful consent in 2015. The whole industry inherited that
                opacity. We&rsquo;re building the opposite.
              </p>
              <Link
                href="/providers"
                className="btn-primary mt-8 bg-white text-primary-700 hover:bg-neutral-50"
              >
                See the audit dashboard
              </Link>
            </div>
            <div className="lg:justify-self-end">
              <TransparencyDemoEmbed />
            </div>
          </div>
        </div>
      </section>

      <ComparisonTable />

      <section className="container-page py-16">
        <div className="mx-auto max-w-4xl">
          <div className="rounded-2xl border border-neutral-200 bg-white p-8 shadow-sm md:p-12">
            <span className="pill bg-primary-50 text-primary-700">
              First customer
            </span>
            <h2 className="h-section mt-4 text-neutral-900">
              Dynolabs vCard fixed its contact-enrichment import
              from 0% to &gt;90% — and pays ~10× less than Proxycurl.
            </h2>
            <p className="mt-4 text-lead">
              vCard&rsquo;s LinkedIn-profile lookups used to hit datacenter-IP
              rate limits and silently return empty. Routing the same fetch
              through iogrid&rsquo;s residential-bandwidth mesh restored the
              field — name, title, company come back end-to-end at p95 ≈ 600 ms.
              vCard is OpenOva&rsquo;s contacts app and the canonical Phase 0
              internal customer.
            </p>
            <ul className="mt-6 grid gap-4 text-sm text-neutral-700 md:grid-cols-3">
              <li className="rounded-lg bg-neutral-50 p-4">
                <strong className="block text-neutral-900">~$0.30 / GB</strong>
                vs. Proxycurl&rsquo;s $0.49 / call — roughly 10× cheaper at
                vCard&rsquo;s expected volume.
              </li>
              <li className="rounded-lg bg-neutral-50 p-4">
                <strong className="block text-neutral-900">p95 ≈ 600 ms</strong>
                vs. ~1 s for incumbent LinkedIn-enrichment SaaS.
              </li>
              <li className="rounded-lg bg-neutral-50 p-4">
                <strong className="block text-neutral-900">Customer-controlled ToS</strong>
                You own destination headers + posture, same model as
                Bright Data / Oxylabs.
              </li>
            </ul>
            <p className="mt-6 text-sm text-neutral-500">
              See the runnable demo at{" "}
              <a
                href="https://github.com/iogrid/iogrid/tree/main/examples/phase0-vcard-customer"
                className="font-medium text-primary-700 hover:underline"
              >
                examples/phase0-vcard-customer
              </a>{" "}
              or the full walkthrough in{" "}
              <a
                href="https://github.com/iogrid/iogrid/blob/main/docs/PHASE0_FIRST_CUSTOMER.md"
                className="font-medium text-primary-700 hover:underline"
              >
                docs/PHASE0_FIRST_CUSTOMER.md
              </a>
              .
            </p>
          </div>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl text-center">
          <h2 className="h-section text-neutral-900">Install in two minutes.</h2>
          <p className="mt-4 text-lead">
            Signed installers for every desktop OS. Daemon footprint is 3 MB of
            RAM. Battery impact on a laptop: negligible.
          </p>
        </div>
        <div className="mx-auto mt-10 max-w-2xl">
          <InstallButtons />
          <p className="mt-4 text-center text-xs text-neutral-500">
            Prefer the terminal?{" "}
            <code className="rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-neutral-700">
              curl -fsSL https://iogrid.org/install/mac | sh
            </code>
          </p>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="rounded-2xl border border-neutral-200 bg-neutral-50 p-8 text-center md:p-12">
          <h2 className="h-section text-neutral-900">
            Buying services? Start with $5.
          </h2>
          <p className="mx-auto mt-4 max-w-2xl text-lead">
            No annual contract. No SDR call. Pay per byte, per minute, or per
            hour. Bring USD, USDC, or $GRID. Audit log included on every
            invoice.
          </p>
          <Link href="/pricing" className="btn-primary mt-8">
            See pricing
          </Link>
        </div>
      </section>
    </>
  );
}
