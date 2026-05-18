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
