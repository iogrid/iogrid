import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Documentation",
  description: "iogrid documentation — provider guides, customer API, daemon internals, $GRID whitepaper.",
};

const sections = [
  {
    title: "For providers",
    href: "https://docs.iogrid.org/providers",
    summary:
      "Install the daemon. Tune the scheduler. Choose your payout currency. Read the audit dashboard.",
  },
  {
    title: "For customers",
    href: "https://docs.iogrid.org/customers",
    summary:
      "API reference for the proxy, compute, GPU, and iOS-build gateways. Code samples in Go, TypeScript, Python.",
  },
  {
    title: "Daemon internals",
    href: "https://docs.iogrid.org/daemon",
    summary:
      "Rust crate workspace, transport layer, anti-abuse mirroring, scheduler state machine. AGPL source.",
  },
  {
    title: "$GRID whitepaper",
    href: "https://docs.iogrid.org/grid",
    summary:
      "Tokenomics, emission schedule, vesting mechanics, governance — and the legal risks we believe are material.",
  },
  {
    title: "Anti-abuse policy",
    href: "https://docs.iogrid.org/aup",
    summary:
      "Categories we route. Categories we refuse. How abuse reports are processed. Provider protections.",
  },
  {
    title: "Architecture",
    href: "https://docs.iogrid.org/architecture",
    summary:
      "Coordinator microservices, daemon-coordinator transport, data plane, federation plan.",
  },
];

export default function DocsLandingPage() {
  return (
    <section className="container-page py-16">
      <header className="mx-auto max-w-3xl text-center">
        <h1 className="h-hero text-neutral-900">Documentation</h1>
        <p className="mt-4 text-lead">
          Full docs live at{" "}
          <Link
            href="https://docs.iogrid.org"
            className="text-primary-600 underline"
          >
            docs.iogrid.org
          </Link>
          . This page is the table of contents.
        </p>
      </header>
      <div className="mx-auto mt-12 grid max-w-5xl gap-6 md:grid-cols-2">
        {sections.map((s) => (
          <a
            key={s.href}
            href={s.href}
            className="card transition hover:border-primary-500 hover:shadow-md"
          >
            <h2 className="h-card text-neutral-900">{s.title}</h2>
            <p className="mt-2 text-sm text-neutral-600">{s.summary}</p>
            <span className="mt-4 inline-block text-sm font-semibold text-primary-600">
              Read &rarr;
            </span>
          </a>
        ))}
      </div>
    </section>
  );
}
