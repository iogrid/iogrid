import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Docker compute — $0.018 / vCPU-hour on home + Mac hardware",
  description:
    "Run any OCI container on idle home PCs and Macs. gVisor-isolated, x86_64 and ARM64, per-second billing. Cheaper than spot.",
};

const STEPS = [
  {
    n: "1",
    title: "Submit a container",
    body: "Push an OCI image reference (ghcr.io, docker.io official-images, or your registry with credentials forwarded) plus resource caps and a category tag.",
  },
  {
    n: "2",
    title: "Scheduler picks a provider",
    body: "The coordinator matches your spec (CPU, RAM, ARM64 vs x86_64, category opt-in) against the live provider pool, weighted by reputation, recent uptime, and load.",
  },
  {
    n: "3",
    title: "Run isolated, stream logs",
    body: "The provider daemon spins the container under gVisor (or Kata), with cgroup limits, a read-only root filesystem, and no host-network egress. Logs stream back via a signed URL.",
  },
];

const PRICING = [
  { col: "vCPU-hour", value: "$0.018" },
  { col: "RAM (per GB-hour)", value: "$0.018" },
  { col: "Bandwidth included", value: "50 GB / job" },
  { col: "Billing granularity", value: "Per second after first minute" },
  { col: "Minimum spend", value: "None" },
  { col: "$GRID discount", value: "20% off list price" },
];

const USE_CASES = [
  {
    title: "Web scraping at residential prices",
    body: "Run headless-browser scrapers in containers that egress through the same mesh — pair with the bandwidth proxy so the requests originate from real residential IPs instead of a flagged datacenter range.",
  },
  {
    title: "Batch data jobs",
    body: "Spin a few hundred containers in parallel for ETL, log compaction, or document conversion. ARM64 providers (Apple Silicon, Raspberry Pi clusters) handle media transcoding particularly well at this price point.",
  },
  {
    title: "CI overflow",
    body: "Burst GitHub Actions overflow onto iogrid when your minute budget is tight. The same OCI image runs unchanged; point your workflow at the iogrid endpoint when the queue is hot.",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "What container runtimes are supported?",
    a: "OCI-conformant images from ghcr.io, docker.io official-images, and any custom registry you authorise. The provider daemon runs Docker locally with gVisor or Kata sandboxing — your image does not see the host kernel.",
  },
  {
    q: "Can my workload reach the public internet?",
    a: "Only through the routed iogrid tunnel, which applies the same anti-abuse filters as the bandwidth proxy. Direct host-network egress is blocked by default.",
  },
  {
    q: "What happens if a provider drops mid-job?",
    a: "Jobs are restartable. The scheduler re-dispatches to another eligible provider within seconds. You are billed only for completed compute, not for the failed attempt.",
  },
  {
    q: "How does pricing compare to spot?",
    a: "Roughly half of AWS spot for general-purpose CPU at the time of writing, and without the eviction risk of spot. The trade-off: you bring your own scheduling logic for very large fan-out (we are not Kubernetes).",
  },
  {
    q: "Is there a free tier?",
    a: "Every new workspace gets $5 of credit. No card required up front; we ask once you exceed it.",
  },
];

const SDK_SNIPPET = `import { IogridClient } from '@iogrid/sdk';

const iogrid = new IogridClient({ apiKey: process.env.IOGRID_API_KEY! });

const w = await iogrid.createWorkload({
  type: 'DOCKER',
  docker: {
    image: 'ghcr.io/example/scraper@sha256:abc...',
    command: ['./run.sh', '--target', 'amazon.com'],
    env: { CONCURRENCY: '4' },
    timeoutSeconds: 900,
    minCpuCores: 2,
    minMemoryMib: 1024,
  },
});

for await (const ev of iogrid.streamWorkloadEvents(w.id)) {
  console.log(\`[\${ev.occurredAt}] \${ev.newStatus}\`);
}`;

export default function ComputePage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Compute"
        title="Run containers at residential prices."
        subtitle="Any OCI image, any architecture. Scheduled across home PCs and Macs with gVisor isolation, per-second billing, and no minimum spend."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Submit a container
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

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What it is
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            iogrid Compute is a workload-scheduler that places Docker containers
            on home PCs and Macs that have opted in to share their idle CPU and
            memory. Workloads run under gVisor (or Kata) with cgroup limits and
            a read-only root filesystem; provider hosts never see your image
            internals and your container never sees the host kernel.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            Unlike datacenter spot capacity, supply does not evict you when a
            larger customer arrives — providers earn at the same rate from
            every workload, so there is no priority bidding war.
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
          <p className="mt-4 text-xs text-muted-foreground">
            Reference range: $0.02 – $0.10 per vCPU-hour depending on category
            and region. See{" "}
            <Link
              href="/pricing"
              className="text-foreground underline-offset-2 hover:underline"
            >
              the pricing page
            </Link>{" "}
            for volume discounts above $500 / month.
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
            The TypeScript SDK is the fastest path; the same shape works in the
            Python, Go, and Java SDKs.
          </p>
          <pre className="mt-6 overflow-x-auto rounded-lg border border-border bg-muted p-6 text-xs leading-relaxed text-foreground">
            <code>{SDK_SNIPPET}</code>
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
