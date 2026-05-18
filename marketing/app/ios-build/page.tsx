import type { Metadata } from "next";
import Link from "next/link";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";

export const metadata: Metadata = {
  title: "iOS build CI — pay-per-minute Mac builds at $0.04 / min",
  description:
    "Half the price of GitHub Actions Mac. No 24-hour leases like AWS EC2 Mac. Ephemeral Tart-spawned VMs on home Mac hardware.",
};

export default function IOSBuildPage() {
  return (
    <>
      <Hero
        eyebrow="iOS build CI"
        title={
          <>
            Mac builds at <span className="text-primary-500">$0.04 / minute</span>.
            No lease. No minimum.
          </>
        }
        subtitle={
          <>
            GitHub Actions Mac is $0.08 / minute. Bitrise and Codemagic start at
            $0.10. AWS EC2 Mac is $26 minimum per session. iogrid is half the
            cheapest no-commit option, with zero floor.
          </>
        }
        primaryCta={{ href: "/pricing", label: "Run a build at $0.04 / min" }}
        secondaryCta={{ href: "#calculator", label: "Compare the math" }}
      />

      <FeatureGrid
        title="Why home Macs work for CI"
        features={[
          {
            title: "Ephemeral Tart VMs",
            body: "Each build runs in a fresh Tart-spawned macOS VM. Hypervisor isolation. The VM is destroyed at job exit.",
          },
          {
            title: "Apple Silicon first",
            body: "M1, M2, M3 providers preferred. Faster compiles than data-center Intel Macs.",
          },
          {
            title: "Latest Xcode",
            body: "Latest 3 Xcode versions live in our pre-baked Tart base images. Older versions on request.",
          },
          {
            title: "GitHub Actions runner",
            body: "Use us as a self-hosted runner with a single labelled workflow. Drop-in replacement.",
          },
          {
            title: "Artifact bucket",
            body: "S3-compatible build artifact bucket included. .ipa, .xcarchive, dSYMs delivered via signed URL.",
          },
          {
            title: "No idle waste",
            body: "Per-second billing after the first minute. A 7-minute build costs $0.28. A 90-second build costs $0.06.",
          },
        ]}
      />

      <section id="calculator" className="container-page py-16">
        <div className="mx-auto max-w-4xl">
          <h2 className="h-section text-center text-neutral-900">
            The math, for an indie iOS dev
          </h2>
          <p className="mt-4 text-center text-lead">
            One commit pushes a fresh build. Assume 10 commits / day, 8 minutes
            per build, 22 working days / month = 1,760 build-minutes.
          </p>
          <div className="mt-12 overflow-x-auto rounded-xl border border-neutral-200 bg-white">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-neutral-200 bg-neutral-50">
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Service
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Per minute
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    1,760 min / month
                  </th>
                  <th className="px-4 py-3 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                    Notes
                  </th>
                </tr>
              </thead>
              <tbody className="font-tabular">
                <tr className="bg-primary-50/70 font-semibold">
                  <td className="px-4 py-3">iogrid</td>
                  <td className="px-4 py-3">$0.04</td>
                  <td className="px-4 py-3">$70</td>
                  <td className="px-4 py-3 text-sm font-normal">Per-second billing, no minimum</td>
                </tr>
                <tr>
                  <td className="px-4 py-3">GitHub Actions Mac</td>
                  <td className="px-4 py-3">$0.08</td>
                  <td className="px-4 py-3">$141</td>
                  <td className="px-4 py-3 text-sm">Subject to rate quota</td>
                </tr>
                <tr className="bg-neutral-50/60">
                  <td className="px-4 py-3">GitHub Actions M-series</td>
                  <td className="px-4 py-3">$0.16</td>
                  <td className="px-4 py-3">$282</td>
                  <td className="px-4 py-3 text-sm">2× cost for faster CPU</td>
                </tr>
                <tr>
                  <td className="px-4 py-3">Bitrise (typical)</td>
                  <td className="px-4 py-3">$0.20</td>
                  <td className="px-4 py-3">$352</td>
                  <td className="px-4 py-3 text-sm">Plus monthly platform fee</td>
                </tr>
                <tr className="bg-neutral-50/60">
                  <td className="px-4 py-3">AWS EC2 Mac (effective)</td>
                  <td className="px-4 py-3">$0.018</td>
                  <td className="px-4 py-3">$32 + lease floor</td>
                  <td className="px-4 py-3 text-sm">Cheaper per minute, BUT $26 / 24-hour minimum each session</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p className="mt-6 text-center text-xs text-neutral-500">
            Your real-world ratio may differ. Indie devs typically save 40–60% / month switching to iogrid from GitHub Actions Mac.
          </p>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="rounded-2xl border border-neutral-200 bg-neutral-50 p-8 text-center md:p-12">
          <h2 className="h-section text-neutral-900">
            Already using GitHub Actions Mac?
          </h2>
          <p className="mx-auto mt-4 max-w-2xl text-lead">
            Switch in five lines of YAML. Self-hosted runner labelled
            <code className="mx-1 rounded bg-neutral-100 px-1.5 py-0.5 font-mono text-sm text-neutral-700">
              iogrid-mac
            </code>
            picks up jobs from your existing workflow.
          </p>
          <Link href="/pricing" className="btn-primary mt-8">
            Get started
          </Link>
        </div>
      </section>
    </>
  );
}
