import type { Metadata } from "next";
import Link from "next/link";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";

export const metadata: Metadata = {
  title: "GPU inference — consumer GPUs at $0.20 / hour",
  description:
    "NVIDIA 4090, 5090, and Apple Silicon MLX inference on idle home hardware. Cheaper than RunPod, Vast, Salad.",
};

export default function GPUPage() {
  return (
    <>
      <Hero
        eyebrow="GPU inference"
        title="Consumer GPUs. Production prices."
        subtitle={
          <>
            Batch inference and fine-tuning on idle 4090s, 5090s, and M3 Max
            Apple Silicon. Hugging Face TGI / vLLM templates included. Bring
            your model, we route the work.
          </>
        }
        primaryCta={{ href: "/pricing", label: "Start at $0.20 / GPU-hour" }}
      />

      <FeatureGrid
        title="Hardware mix"
        features={[
          {
            title: "NVIDIA 4090 / 5090",
            body: "24+ GB VRAM consumer cards from gamer providers. Great for 7B–34B models, batch inference, LoRA fine-tunes.",
          },
          {
            title: "Apple Silicon MLX",
            body: "M3 Max and M4 Macs running MLX. Especially good for Mistral, Llama, and Whisper transcription.",
          },
          {
            title: "Per-second billing",
            body: "First minute is rounded; after that, billed per second. Best-fit pricing for short inference bursts.",
          },
          {
            title: "Pre-flight benchmark",
            body: "We benchmark each provider&rsquo;s GPU before dispatch and only charge once we&rsquo;ve confirmed advertised performance.",
          },
          {
            title: "Templates",
            body: "vLLM, Hugging Face TGI, Ollama, ComfyUI, AUTOMATIC1111 — start a job with a template name, no Dockerfile needed.",
          },
          {
            title: "BYO weights",
            body: "Mount weights from S3 or HF Hub. Provider hardware can&rsquo;t exfiltrate model parameters — VRAM is wiped at job exit.",
          },
        ]}
      />

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">When to use us</h2>
          <ul className="mt-6 space-y-3 text-neutral-700">
            <li>
              <strong className="text-neutral-900">Yes:</strong> batch
              inference (parallel embedding, document scoring, image
              generation), short fine-tunes, RAG re-ranking, audio transcription.
            </li>
            <li>
              <strong className="text-neutral-900">Yes:</strong> bursty
              workloads that don&rsquo;t fit a 24/7 reserved-capacity contract.
            </li>
            <li>
              <strong className="text-neutral-900">Maybe:</strong> latency-sensitive
              real-time inference. Best-effort SLA in Phase 1; Phase 2 ships
              region-pinned reserved capacity.
            </li>
            <li>
              <strong className="text-neutral-900">No (yet):</strong> training
              from scratch on H100-class capacity. We&rsquo;re consumer-GPU first
              — see RunPod or Lambda for H100/B200 needs.
            </li>
          </ul>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="rounded-2xl border border-neutral-200 bg-neutral-50 p-8 text-center md:p-12">
          <h2 className="h-section text-neutral-900">Try one inference job</h2>
          <p className="mx-auto mt-4 max-w-2xl text-lead">
            First $5 is on us. Run a vLLM job against Llama 3 70B and tell us
            how the latency compares.
          </p>
          <Link href="/pricing" className="btn-primary mt-8">
            See pricing
          </Link>
        </div>
      </section>
    </>
  );
}
