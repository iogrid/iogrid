import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Transparency reports",
  description:
    "iogrid quarterly transparency reports. Total filter checks, block rate, law-enforcement inquiries received and responded to, and audit-retention compliance. Published every quarter per docs/LEGAL.md.",
  openGraph: {
    title: "iogrid transparency reports",
    description:
      "Quarterly transparency reports: filter activity, LE inquiries, audit-retention compliance.",
  },
  alternates: { canonical: "/transparency/" },
  robots: { index: true, follow: true },
};

// Static placeholder shipped with the build. Once antiabuse-svc's
// CronJob has produced its first real report (Q1 2026) the marketing
// site will fetch live data from /status/transparency via the
// gateway-bff and render dynamically; for the initial Phase-1 launch we
// show the methodology so visitors understand what the eventual
// numbers will mean.
const placeholderReports = [
  {
    label: "Q1 2026",
    status: "placeholder",
    description:
      "Methodology-only placeholder. First real report covers calendar Q1 2026 (1 January 2026 – 31 March 2026) and will publish in early April 2026.",
  },
];

export default function TransparencyPage() {
  return (
    <article className="container-page py-16">
      <div className="mx-auto max-w-3xl">
        <span className="pill bg-primary-50 text-primary-700">
          Quarterly cadence
        </span>
        <h1 className="mt-4 text-4xl font-extrabold tracking-tight text-neutral-900 md:text-5xl">
          Transparency reports
        </h1>
        <p className="mt-4 text-lg text-neutral-700">
          Every quarter iogrid publishes the totals our anti-abuse pipeline
          processed, the law-enforcement inquiries we received, and our
          audit-log retention compliance. The full schema lives in{" "}
          <a
            className="text-primary-600 underline"
            href="https://github.com/iogrid/iogrid/blob/main/docs/LEGAL.md"
          >
            <code>docs/LEGAL.md</code>
          </a>
          .
        </p>

        <section className="mt-12 space-y-6 text-neutral-700">
          <h2 className="h-section text-neutral-900">What we report</h2>
          <ul className="list-inside list-disc space-y-2">
            <li>
              <strong>Filter activity</strong> — total pre-flight checks performed,
              total blocks issued, aggregate block rate, broken down by category
              (CSAM hash match, phishing, fraud, port-policy, rate-limit,
              destination deny-list, etc.).
            </li>
            <li>
              <strong>Backend hit-rates</strong> — per-feed hit-rate for NCMEC
              PhotoDNA, PhishTank, OpenPhish, and Google Safe Browsing so the
              relative value of each feed is visible.
            </li>
            <li>
              <strong>Law-enforcement engagement</strong> — number of inquiries
              received, responses sent, breakdown by jurisdiction and request
              type (subpoena, MLAT, warrant, informal). Requests subject to
              non-disclosure orders are counted in aggregate without detail.
            </li>
            <li>
              <strong>Audit-retention compliance</strong> — required vs.
              configured retention, oldest record present, most recent pruning
              pass and the row count it removed.
            </li>
            <li>
              <strong>Methodology</strong> — exactly how the numbers are
              produced so they can be reproduced or audited.
            </li>
          </ul>

          <h2 className="h-section text-neutral-900">Publishing cadence</h2>
          <p>
            Reports cover calendar quarters and publish within fourteen days of
            quarter-end. The Q1 report covers January through March; Q2 covers
            April through June; Q3 covers July through September; Q4 covers
            October through December.
          </p>

          <h2 className="h-section text-neutral-900">Reports</h2>
          <ul className="space-y-3">
            {placeholderReports.map((r) => (
              <li
                key={r.label}
                className="rounded-md border border-neutral-200 p-4"
              >
                <div className="flex items-center justify-between">
                  <span className="text-lg font-semibold text-neutral-900">
                    {r.label}
                  </span>
                  <span className="pill bg-warning/20 text-amber-700">
                    {r.status}
                  </span>
                </div>
                <p className="mt-2 text-sm text-neutral-600">
                  {r.description}
                </p>
                <div className="mt-3 flex flex-wrap gap-3 text-sm">
                  <a
                    className="text-primary-600 underline"
                    href={`/status/transparency/${
                      r.label.split(" ")[1]
                    }/${r.label[1]}`}
                  >
                    JSON
                  </a>
                  <a
                    className="text-primary-600 underline"
                    href={`/status/transparency/${
                      r.label.split(" ")[1]
                    }/${r.label[1]}.md`}
                  >
                    Markdown
                  </a>
                </div>
              </li>
            ))}
          </ul>

          <h2 className="h-section text-neutral-900">Warrant canary</h2>
          <p>
            A warrant-canary policy is targeted for Phase 3 per{" "}
            <a
              className="text-primary-600 underline"
              href="https://github.com/iogrid/iogrid/blob/main/docs/LEGAL.md"
            >
              <code>docs/LEGAL.md</code>
            </a>
            . Until it ships, the absence of warrant disclosure in a report
            should NOT be interpreted as either presence or absence of one.
          </p>
        </section>
      </div>
    </article>
  );
}
