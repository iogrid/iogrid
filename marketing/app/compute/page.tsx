import type { Metadata } from "next";
import Link from "next/link";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";

export const metadata: Metadata = {
  title: "Docker compute — run containers on idle home hardware",
  description:
    "OCI containers on gVisor-isolated Linux + Mac providers. $0.018 per vCPU-hour. Cheaper than spot, no AWS lock-in.",
};

export default function ComputePage() {
  return (
    <>
      <Hero
        eyebrow="Docker compute"
        title="Containers on idle hardware. Cheaper than spot."
        subtitle={
          <>
            Submit any OCI image. We dispatch it to a Linux or Mac provider with
            free capacity, kernel-isolated via gVisor or Kata. Bring your image,
            your inputs, your outputs. We bring the iron.
          </>
        }
        primaryCta={{ href: "/pricing", label: "Start at $0.018 / vCPU-hour" }}
      />

      <FeatureGrid
        title="What you can run"
        features={[
          {
            title: "Batch processing",
            body: "Video transcoding, image generation, ETL pipelines, any workload that fits a container.",
          },
          {
            title: "ML inference",
            body: "CPU-bound inference for smaller models. Use the GPU tier for larger models.",
          },
          {
            title: "Web scraping helpers",
            body: "Browserless / headless Chrome containers running alongside our proxy network.",
          },
          {
            title: "Periodic jobs",
            body: "Cron-equivalent scheduling. Pay only for execution time. No idle cluster cost.",
          },
          {
            title: "Custom CI runners",
            body: "Self-hosted GitHub Actions / GitLab runner images. We provide the capacity.",
          },
          {
            title: "Build farms",
            body: "Yocto, Bazel, distributed compilers — pin to ARM64 or x86_64, scale to dozens of providers.",
          },
        ]}
      />

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">Isolation, in detail</h2>
          <ul className="mt-6 space-y-3 text-neutral-700">
            <li>
              <strong className="text-neutral-900">Linux providers:</strong>{" "}
              gVisor (Google&rsquo;s userspace kernel) or Kata Containers
              (lightweight VM per container). Provider&rsquo;s host kernel is
              shielded; container escapes are blocked at the gVisor sentry.
            </li>
            <li>
              <strong className="text-neutral-900">Mac providers:</strong> Each
              container runs inside Docker Desktop&rsquo;s lightweight VM. The
              provider sees CPU + RAM usage but not container internals.
            </li>
            <li>
              <strong className="text-neutral-900">Resource caps:</strong>{" "}
              CPU, RAM, GPU, bandwidth, runtime are all capped per workload. A
              provider&rsquo;s machine is never throttled below the floor they
              configured in their dashboard.
            </li>
            <li>
              <strong className="text-neutral-900">Network policy:</strong>{" "}
              By default, your container can reach the public internet via the
              provider&rsquo;s IP and your job&rsquo;s S3 bucket. Inbound is
              denied. Outbound to provider&rsquo;s LAN is denied.
            </li>
          </ul>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="rounded-2xl border border-neutral-200 bg-neutral-50 p-8 text-center md:p-12">
          <h2 className="h-section text-neutral-900">Submit a container</h2>
          <p className="mx-auto mt-4 max-w-2xl text-lead">
            Bring an image. Bring a CLI. Pay per second after the first minute.
            Logs and exit code stream to your dashboard in real time.
          </p>
          <Link href="/pricing" className="btn-primary mt-8">
            See pricing
          </Link>
        </div>
      </section>
    </>
  );
}
