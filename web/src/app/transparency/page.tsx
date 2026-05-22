import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Transparency",
  description:
    "Quarterly public-facing transparency reports. Aggregate stats on traffic categories, legal requests, network growth, $GRID emissions and burns. No PII, no customer identifiers.",
};

const Q2_SECTIONS = [
  {
    title: "Treasury balance",
    body: "USDC operating reserve, $GRID held by Squads multisig (3-of-5), SOL gas reserve. Q2 figures TBD — final snapshot taken on 2026-06-30 23:59 UTC.",
  },
  {
    title: "Emission this quarter",
    body: "Year-0 schedule emits 50M $GRID across the year (≈137k / day). Actual vs curve is reported with the snapshot. Emission is hard-coded into the SPL program; no governance can override.",
  },
  {
    title: "Buyback-and-burn",
    body: "Target ≥2% of monthly revenue routed through Jupiter into the well-known incinerator address. Live dashboard at burn.iogrid.org once TGE lands. Top 5 burn transactions surfaced per quarter.",
  },
  {
    title: "Staking participation",
    body: "Total $GRID staked, % of circulating and total supply, distinct stakers, active provider nodes. Mandatory provider lockup pool sits inside this number.",
  },
  {
    title: "Liquidity health",
    body: "Raydium CLMM $GRID/USDC pool — active liquidity (USD-equivalent), 30-day volume, concentration within ±5% of mid, foundation-owned LP position (locked 4 years via Streamflow). Slippage benchmark for a $10k market buy.",
  },
  {
    title: "Foundation activity",
    body: "Grants disbursed, partnerships announced, governance proposals, headcount changes. Material regulatory developments and audits get their own subsection.",
  },
  {
    title: "Compliance + forward look",
    body: "Material regulatory developments, audits in progress / completed, jurisdictional posture changes, litigation / subpoenas / enforcement contact. Forward look for the next quarter (no price expectations).",
  },
];

export default function TransparencyPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Transparency"
        title="Quarterly public-facing transparency reports."
        subtitle="Aggregate stats on traffic categories, legal requests, network growth, $GRID emissions and burns, treasury balance, and liquidity health. No PII, no customer identifiers."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Current quarter
          </h2>
          <div className="mt-6 rounded-lg border border-border bg-background p-6">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h3 className="text-lg font-semibold text-foreground">
                $GRID transparency report &mdash; 2026 Q2
              </h3>
              <span className="rounded-full border border-border px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
                DRAFT
              </span>
            </div>
            <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">
                  Reporting period
                </dt>
                <dd className="text-foreground">2026-04-01 &rarr; 2026-06-30</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">
                  Publish date
                </dt>
                <dd className="text-foreground">2026-07-01 (scheduled)</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">
                  Report author
                </dt>
                <dd className="text-foreground">iogrid Foundation</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">
                  Prior report
                </dt>
                <dd className="text-muted-foreground">&mdash; (inaugural)</dd>
              </div>
            </dl>
            <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
              This is the first $GRID transparency report. Most figures are
              still TBD and will be filled progressively through Q2 2026, with
              the final snapshot taken on 2026-06-30 and the report flipped to{" "}
              <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                PUBLISHED
              </code>{" "}
              on 2026-07-01. Until then, the authoritative source is the
              on-chain data itself.
            </p>
            <div className="mt-6 flex flex-wrap gap-3">
              <Link
                href="https://github.com/iogrid/iogrid/blob/main/docs/transparency/2026-Q2.md"
                className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
              >
                Read the full draft
              </Link>
              <Link
                href="https://github.com/iogrid/iogrid/blob/main/docs/transparency/TEMPLATE.md"
                className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
              >
                Report template
              </Link>
            </div>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What every report covers
          </h2>
          <dl className="mt-8 space-y-6">
            {Q2_SECTIONS.map((s) => (
              <div key={s.title}>
                <dt className="text-base font-semibold text-foreground">
                  {s.title}
                </dt>
                <dd className="mt-2 text-base leading-relaxed text-muted-foreground">
                  {s.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Methodology
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Every figure cites the RPC endpoint, the block / slot range, and
            the commands used to derive it (typically{" "}
            <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
              spl-token accounts
            </code>{" "}
            against the multisig, a Raydium SDK decode of the pool account,
            and a custom RPC against the iogrid staking program state).
            Corrections are appended to the report after publication; the
            original numbers stay visible for audit.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link
              href="https://github.com/iogrid/iogrid/tree/main/docs/transparency"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              View all reports
            </Link>
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
