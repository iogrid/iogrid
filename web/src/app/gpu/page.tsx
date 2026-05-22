import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "GPU inference",
  description: "Run LLM, vision, and audio inference on consumer GPUs. NVIDIA + Apple Silicon (MLX). Per-second billing.",
};

export default function GpuPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="GPU"
        title="GPU inference, anywhere there is idle silicon."
        subtitle="Run LLM, vision, and audio inference on consumer GPUs. NVIDIA + Apple Silicon (MLX). Per-second billing."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Bring an OCI image, declare GPU memory needs, get scheduled. We handle the scoring and routing.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
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
    </MarketingShell>
  );
}
