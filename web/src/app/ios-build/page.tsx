import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "iOS build CI — $0.04 / Xcode-minute on real Macs",
  description:
    "Per-minute Mac CI on home Apple Silicon. Ephemeral Tart VMs reset between jobs. No 24-hour lease minimum. ~50% cheaper than GitHub Actions.",
};

const STEPS = [
  {
    n: "1",
    title: "Push your project to a signed S3 URL",
    body: "The coordinator gives you a short-lived signed PUT URL. Tarball your source, push it, and reference it in the workload spec along with a Tart image (Xcode 15 / 16 / 17 preinstalled).",
  },
  {
    n: "2",
    title: "Scheduler picks a Mac provider",
    body: "Only providers with the Mac platform, Tart installed, and the requested Xcode version eligible see the job. Apple Silicon (M1 / M2 / M3 / M4) is preferred for speed.",
  },
  {
    n: "3",
    title: "Ephemeral VM, archive, upload, destroy",
    body: "The daemon spawns a Tart VM with no provider-host filesystem access, clones your tarball, runs xcodebuild, archives the .ipa or .xcarchive into your artefact bucket, and destroys the VM at the end.",
  },
];

const PRICING = [
  { col: "Per Xcode-minute", value: "$0.04" },
  { col: "Lease minimum", value: "None (per-minute)" },
  { col: "Xcode versions", value: "Provider-installed (15, 16, 17 typical)" },
  { col: "Network egress", value: "Routed via iogrid mesh — no per-GB tax" },
  { col: "Artefact bucket", value: "S3-compatible, signed URLs included" },
  { col: "$GRID discount", value: "20% off list price" },
];

const COMPARISON = [
  { vendor: "iogrid", price: "$0.04 / min", lease: "None" },
  { vendor: "GitHub Actions macos-latest", price: "$0.08 / min", lease: "None" },
  { vendor: "GitHub Actions macos-latest-large", price: "$0.16 / min", lease: "None" },
  { vendor: "Bitrise", price: "$0.10 – $0.30 / min", lease: "None" },
  { vendor: "Codemagic", price: "$0.10 – $0.20 / min", lease: "None" },
  { vendor: "CircleCI macOS", price: "$0.10 – $0.15 / min", lease: "None" },
  { vendor: "MacStadium dedicated", price: "$0.05 – $0.20 / min effective", lease: "Long-term lease" },
  { vendor: "AWS EC2 Mac", price: "$0.018 / min effective", lease: "24-hour minimum ($26 floor)" },
];

const USE_CASES = [
  {
    title: "Indie iOS app CI",
    body: "Build, test, archive, and ship to TestFlight on every push. Indie devs save $30–80/month vs GitHub Actions; teams running hundreds of nightly builds save proportionally more.",
  },
  {
    title: "Cross-platform mobile (Flutter / RN / Expo)",
    body: "Run the iOS half of your matrix on iogrid and the Android / web half wherever you like. Per-minute billing means a 4-minute Flutter pod-install + build does not pay for unused capacity.",
  },
  {
    title: "Open-source library CI",
    body: "Test SwiftPM / CocoaPods packages against multiple Xcode versions without committing to a paid Mac plan. The pre-flight benchmark catches a broken Xcode install on the provider before you are charged.",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "Why is this half the price of GitHub Actions?",
    a: "GitHub Actions runs on rented data-centre Mac minis. iogrid runs on home Macs that would otherwise sit idle — the provider only needs the marginal electricity to earn, no capital depreciation, no rack fees. A Mac provider with 4 idle hours per day earns ~$145 / month at our rates, ~15× what they would on a bandwidth-only network.",
  },
  {
    q: "Is this isolated enough for proprietary code?",
    a: "Each build runs in an ephemeral Tart VM with no access to the provider's host filesystem. The VM is destroyed when the build finishes. Tart is the same technology Cirrus CI uses for their macOS runners. Your source tarball is encrypted in transit and at rest, and only the assigned provider gets a signed pull URL.",
  },
  {
    q: "How does this compare to MacStadium leases?",
    a: "MacStadium is competitive on $/minute only if you keep the lease saturated 24/7. For bursty workloads (typical CI), iogrid is much cheaper because you pay only when builds run. For 100% utilisation steady-state, a dedicated lease still wins.",
  },
  {
    q: "Which Xcode versions are available?",
    a: "Whatever providers have installed. Latest 2–3 versions are typically available across the network within days of release. The job spec includes a Tart image label; the scheduler matches to providers with that image cached.",
  },
  {
    q: "Can I bring my own Xcode license or Apple ID?",
    a: "You do not need to. The provider's Mac is licensed under their own Apple ID; xcodebuild does not require a developer account to compile. For signing, push your distribution certificate as a build secret — it never leaves the ephemeral VM.",
  },
];

const CI_SNIPPET = `# .github/workflows/ios-iogrid.yml — burst-overflow to iogrid
jobs:
  build:
    runs-on: iogrid-ios
    container:
      image: ghcr.io/cirruslabs/macos-sonoma-xcode:latest
    steps:
      - uses: actions/checkout@v4
      - run: |
          xcodebuild \\
            -workspace MyApp.xcworkspace \\
            -scheme MyApp \\
            -configuration Release \\
            -archivePath build/MyApp.xcarchive \\
            archive`;

const SDK_SNIPPET = `import { IogridClient } from '@iogrid/sdk';

const iogrid = new IogridClient({ apiKey: process.env.IOGRID_API_KEY! });

const w = await iogrid.createWorkload({
  type: 'IOS_BUILD',
  iosBuild: {
    sourceTarballS3Key: 'builds/2026-05-22/myapp-abc123.tgz',
    tartImage: 'ghcr.io/cirruslabs/macos-sonoma-xcode:latest',
    buildCommands: [
      'xcodebuild -workspace MyApp.xcworkspace -scheme MyApp archive',
    ],
    artifactS3Bucket: 'myorg-ios-artifacts',
    artifactS3Prefix: 'builds/2026-05-22/',
  },
});`;

export default function IosBuildPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="iOS Build CI"
        title="Half-price Xcode CI on native macOS hardware."
        subtitle="Real Mac runners with Xcode preinstalled. Tart-managed VMs reset between jobs. $0.04 / Xcode-minute — ~50% cheaper than GitHub Actions, no 24-hour lease minimum."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Run a build
            </Link>
            <Link
              href="/install"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Become a Mac provider
            </Link>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What it is
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            iogrid iOS Build CI is per-minute Xcode build capacity on home
            Apple Silicon Macs that have opted in to share idle hours. Each
            build runs in an ephemeral Tart VM that is destroyed when the
            build finishes — providers never see your source or your signing
            certificates.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            We are the cheapest no-commit option in the market and the only
            mainstream iOS CI on truly home hardware. Indie devs save $30–80
            per month immediately; teams running large CI matrices save
            proportionally more.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            How it works
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {STEPS.map((s) => (
              <div key={s.n} className="flex flex-col gap-3 bg-background p-8">
                <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  Step {s.n}
                </span>
                <h3 className="text-base font-semibold text-foreground">
                  {s.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {s.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Pricing
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <tbody className="divide-y divide-border bg-background">
                {PRICING.map((row) => (
                  <tr key={row.col}>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.col}
                    </td>
                    <td className="px-4 py-3 text-foreground">{row.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-4xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            How we compare
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Vendor
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Price / Xcode-minute
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Lease minimum
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-background">
                {COMPARISON.map((row) => (
                  <tr key={row.vendor}>
                    <td className="px-4 py-3 text-foreground">{row.vendor}</td>
                    <td className="px-4 py-3 text-foreground">{row.price}</td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.lease}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            AWS EC2 Mac is cheaper per-minute on paper but requires a 24-hour
            lease ($26 floor), which makes per-build economics worse for any
            workload under ~11 hours per day.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What you can do with it
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {USE_CASES.map((u) => (
              <div key={u.title} className="flex flex-col gap-3 bg-background p-8">
                <h3 className="text-base font-semibold text-foreground">
                  {u.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {u.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Use the SDK directly, or point a GitHub Actions runner label at
            iogrid for drop-in overflow.
          </p>
          <pre className="mt-6 overflow-x-auto rounded-lg border border-border bg-muted p-6 text-xs leading-relaxed text-foreground">
            <code>{SDK_SNIPPET}</code>
          </pre>
          <pre className="mt-4 overflow-x-auto rounded-lg border border-border bg-muted p-6 text-xs leading-relaxed text-foreground">
            <code>{CI_SNIPPET}</code>
          </pre>
          <div className="mt-6 flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Get an API key
            </Link>
            <Link
              href="/docs"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read the SDK docs
            </Link>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            FAQ
          </h2>
          <dl className="mt-8 space-y-8">
            {FAQ.map((row) => (
              <div key={row.q}>
                <dt className="text-base font-semibold text-foreground">
                  {row.q}
                </dt>
                <dd className="mt-2 text-base leading-relaxed text-muted-foreground">
                  {row.a}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </MarketingShell>
  );
}
