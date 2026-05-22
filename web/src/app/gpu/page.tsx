import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "GPU inference — $0.20 / GPU-hour on consumer + Apple Silicon",
  description:
    "Run LLM, vision, and audio inference on NVIDIA consumer cards and Apple Silicon MLX. Per-second billing, pre-flight benchmark, bring your own weights.",
};

const STEPS = [
  {
    n: "1",
    title: "Declare your GPU requirement",
    body: "Pick a VRAM floor (e.g. 24 GB+ for a 13B model in 4-bit) and optionally an allowed-vendor list (NVIDIA, Apple Silicon).",
  },
  {
    n: "2",
    title: "Scheduler finds matching silicon",
    body: "The coordinator picks an opted-in provider with enough free VRAM, matching vendor, and reachable network. Apple Silicon uses Apple's Virtualization framework + MLX bindings; NVIDIA uses the NVIDIA Container Toolkit.",
  },
  {
    n: "3",
    title: "Pre-flight benchmark, then charge",
    body: "The container runs a short benchmark to confirm performance against the spec — if it fails (overheating, contention), the job is rescheduled at no charge. After pass, per-second billing starts.",
  },
];

const PRICING = [
  { col: "Consumer NVIDIA (24 GB VRAM, e.g. 4090)", value: "$0.20 / GPU-hour" },
  { col: "Apple Silicon MLX (M3 Max, M4)", value: "$0.20 / GPU-hour" },
  { col: "Pro / data-center class", value: "Up to $2.00 / GPU-hour" },
  { col: "Billing granularity", value: "Per-second after first minute" },
  { col: "Pre-flight benchmark", value: "Free (only billed if pass)" },
  { col: "$GRID discount", value: "20% off list price" },
];

const USE_CASES = [
  {
    title: "Batch LLM inference",
    body: "Generate embeddings, summarise long documents, run open-weight models (Llama, Mistral, Qwen). 24 GB VRAM holds a 13B model in 4-bit quantisation comfortably.",
  },
  {
    title: "Computer vision pipelines",
    body: "Object detection, segmentation, OCR over image and video archives. Bring a Hugging Face TGI or vLLM template, point it at your S3 bucket, fan out.",
  },
  {
    title: "Fine-tuning small models",
    body: "LoRA / QLoRA fine-tunes on consumer cards. Cheaper than reserving a Lambda Labs instance for a few hours, and the pre-flight benchmark protects you from thermally throttled hardware.",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "Which GPUs are in the network?",
    a: "Consumer NVIDIA cards with 24 GB+ VRAM (4090, 5090) and Apple Silicon (M3 Max, M4 Max, M4 Ultra). Pro and data-center class (A100, H100) are available from a smaller pool at proportionally higher rates.",
  },
  {
    q: "Can I run my own model weights?",
    a: "Yes. Bring an OCI image that pulls weights from your S3 bucket or Hugging Face — provider hosts never inspect your image. Hugging Face TGI and vLLM are the most common templates.",
  },
  {
    q: "What is the pre-flight benchmark?",
    a: "Before charging, the container runs a short workload-shaped probe to verify the chosen GPU is actually performing to spec. If it fails (thermal throttle, VRAM fragmentation, driver issue), the scheduler picks a different provider at no charge.",
  },
  {
    q: "How does Apple Silicon compare to NVIDIA?",
    a: "Unified memory means an M4 Max with 128 GB can hold larger models than a 24 GB 4090, but raw throughput on common kernels is lower. MLX-tuned workloads close most of that gap. Pick by model size and target latency.",
  },
  {
    q: "Is this real-time-suitable?",
    a: "Not as a primary endpoint — there is variable scheduling latency. Use for batch, fine-tuning, or behind a queue. For latency-sensitive endpoints, pair iogrid with a small datacenter footprint and burst overflow to iogrid.",
  },
];

const SDK_SNIPPET = `import { IogridClient } from '@iogrid/sdk';

const iogrid = new IogridClient({ apiKey: process.env.IOGRID_API_KEY! });

const w = await iogrid.createWorkload({
  type: 'GPU',
  gpu: {
    image: 'ghcr.io/huggingface/text-generation-inference:latest',
    env: { MODEL_ID: 'mistralai/Mistral-7B-Instruct-v0.3' },
    timeoutSeconds: 3600,
    minVramMib: 24576,
    allowedVendors: ['NVIDIA', 'APPLE'],
  },
});

const { result } = await iogrid.getWorkload(w.id);
console.log(result?.terminalStatus, result?.cost);`;

export default function GpuPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="GPU"
        title="GPU inference, anywhere there is idle silicon."
        subtitle="Run LLM, vision, and audio workloads on consumer NVIDIA cards and Apple Silicon MLX. Per-second billing, pre-flight benchmark, bring your own weights."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Run inference
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
            iogrid GPU is a workload-scheduler for CUDA and MLX containers
            running on idle consumer-class GPUs — NVIDIA 4090 and 5090 class
            cards, plus Apple Silicon with MLX. The same SDK and API as Docker
            compute; the scheduler just additionally matches on VRAM and
            vendor.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            Designed for batch inference, embedding generation, fine-tuning,
            and other GPU-bursty workloads where 5–60 second scheduling
            latency is acceptable in exchange for ~10× lower per-hour cost
            than a reserved datacenter GPU.
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
            Reference range: $0.20 – $2.00 per GPU-hour, tiered by GPU class
            (consumer / pro / data-center). Provider payout: $0.05 – $0.50 per
            GPU-hour depending on tier.
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
            Same SDK as Docker compute — only the workload type and the GPU
            spec change.
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
