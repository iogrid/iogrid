import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "$GRID — deflationary work-token on Solana",
  description:
    "1B supply, halving every 2 years, 2% of revenue burned, 4-year LP lock. Earned by providers, paid by customers for a 20% discount.",
};

const STEPS = [
  {
    n: "1",
    title: "Customers pay in USD, USDC, or $GRID",
    body: "Stripe USD and on-chain USDC settle at list price. On-chain $GRID payments get a 20% discount and route directly to providers and the burn wallet.",
  },
  {
    n: "2",
    title: "98% to providers, 2% buyback-burn",
    body: "Of every dollar of revenue, 2% is converted to $GRID via Jupiter on Solana and burned to the well-known incinerator address. The remaining 98% is converted to $GRID via TWAP and distributed to providers in proportion to their contribution.",
  },
  {
    n: "3",
    title: "Provider lockup + optional bonus tiers",
    body: "Every $GRID earned enters a 30-day cliff + 60-day linear vest. Providers can opt in to longer lockups (up to 1-year cliff + 2-year vest) for up to 2× multiplier on earnings.",
  },
];

const PARAMS = [
  { col: "Symbol", value: "$GRID" },
  { col: "Network", value: "Solana (SPL Token-2022)" },
  { col: "Initial supply", value: "1,000,000,000 (1 billion)" },
  { col: "Decimals", value: "9 (Solana standard)" },
  { col: "Emission curve", value: "Halving every 2 years" },
  { col: "Year-1 emission", value: "50M $GRID (5% of supply)" },
  { col: "Burn-rate target", value: "≥2% of monthly revenue → buyback → burn" },
  { col: "Treasury custody", value: "3-of-5 Squads Protocol multisig" },
  { col: "Liquidity venue", value: "Raydium CLMM $GRID/USDC, LP locked 4 years" },
];

const ALLOCATION = [
  { slice: "Provider rewards pool", pct: "50%", supply: "500M", terms: "Vested linear over 10 years (halving baked in)" },
  { slice: "Team", pct: "15%", supply: "150M", terms: "4-year vest, 1-year cliff" },
  { slice: "Treasury / Governance", pct: "10%", supply: "100M", terms: "Multisig-controlled" },
  { slice: "Strategic investors", pct: "10%", supply: "100M", terms: "12-month cliff, 24-month linear vest" },
  { slice: "Community / ecosystem", pct: "10%", supply: "100M", terms: "Airdrops, bounties, grants, validator rewards" },
  { slice: "Initial DEX liquidity", pct: "5%", supply: "50M", terms: "Paired with USDC on Raydium at TGE" },
];

const EMISSION = [
  { years: "0 – 2", rate: "50M / year" },
  { years: "2 – 4", rate: "25M / year" },
  { years: "4 – 6", rate: "12.5M / year" },
  { years: "6 – 8", rate: "6.25M / year" },
  { years: "8 – 10", rate: "3.125M / year" },
  { years: "10+", rate: "0 — only burns remove supply" },
];

const LOCKUP = [
  { tier: "Standard (default)", schedule: "30-day cliff + 60-day linear vest", multiplier: "1.0×" },
  { tier: "Loyalty", schedule: "90-day cliff + 180-day linear vest", multiplier: "1.25×" },
  { tier: "Conviction", schedule: "180-day cliff + 365-day linear vest", multiplier: "1.5×" },
  { tier: "Maximum", schedule: "365-day cliff + 730-day linear vest", multiplier: "2.0×" },
];

const USE_CASES = [
  {
    title: "Provider work-token",
    body: "Earn $GRID by sharing bandwidth, CPU, GPU, or Mac minutes. Lockup-tier multiplier rewards long-term providers and protects the float from day-1 dumps.",
  },
  {
    title: "Customer pay-with-discount",
    body: "Pay invoices in $GRID and the gateway applies a 20% discount. Tokens flow through to providers plus the 2% burn — persistent buy-pressure as customers swap USD into $GRID to capture the discount.",
  },
  {
    title: "Stake-for-routing-priority",
    body: "Providers staking $GRID earn additional routing-priority weight. Customer-side staking unlocks volume discounts of up to 25% off list price (minimum 30 days).",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "Is $GRID a security?",
    a: "iogrid Foundation operates from the Cayman Islands and geo-blocks US persons at launch. Strategic investor allocations use Reg D / Reg S structuring. The buyback-burn and halving are hard-coded into the SPL emission program, not subject to discretionary governance. None of that is legal advice — consult your own counsel.",
  },
  {
    q: "Can iogrid rug-pull the liquidity pool?",
    a: "No. The initial 5% supply seeded into the Raydium CLMM pool is paired with USDC and the LP position is locked for 4 years via a Streamflow vesting contract. At end of vest the LP tokens are permanently burned, leaving the pool unliftable forever. Verification procedure is in the transparency report.",
  },
  {
    q: "Where can I trade $GRID?",
    a: "The canonical venue is the Raydium CLMM $GRID/USDC pool on Solana. All routing — through Sociable Cash, MoonPay, or any other off-ramp — discovers liquidity through Jupiter, which surfaces this pool as the primary venue. CEX listings are aspirational, not blocking.",
  },
  {
    q: "Why is provider $GRID locked up?",
    a: "Without lockup every provider would convert $GRID to USDC the moment they receive it, crashing the price. The rolling 30/90-day vest keeps ~67% of any month's earnings unsellable at any time, dampening sell-pressure and aligning providers with long-term network success.",
  },
  {
    q: "What happens to revenue from customers paying in fiat?",
    a: "2% is converted to $GRID via Jupiter and burned. 98% is converted to $GRID via TWAP and distributed to providers. Customers paying directly in $GRID get a 20% discount, and that $GRID flows directly to providers and the burn wallet — no swap step.",
  },
];

export default function TokenPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Token"
        title="$GRID — earn it, spend it, lock it."
        subtitle="A deflationary work-token on Solana. 1B supply, halving every 2 years, 2% of revenue burned, 4-year LP lock. Earned by providers, paid by customers for a 20% discount."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/burn"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Live burn dashboard
            </Link>
            <Link
              href="/transparency"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read the transparency report
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
            $GRID is the native unit of account for iogrid: a Solana SPL token
            with a fixed 1 billion supply, halving emission every 2 years, and
            a 2%-of-revenue buyback-burn. Providers earn $GRID by contributing
            bandwidth, compute, GPU, or Mac minutes; customers earn a 20%
            discount by paying invoices in $GRID directly.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            The token is designed to be deflationary on three vectors at once:
            burn, halving, and a mandatory provider-earnings lockup that
            removes most newly-emitted $GRID from the float for at least 90
            days after each payout.
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
            Headline parameters
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <tbody className="divide-y divide-border bg-background">
                {PARAMS.map((row) => (
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
            Token allocation
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Slice
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    %
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Supply
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Terms
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-background">
                {ALLOCATION.map((row) => (
                  <tr key={row.slice}>
                    <td className="px-4 py-3 text-foreground">{row.slice}</td>
                    <td className="px-4 py-3 text-foreground">{row.pct}</td>
                    <td className="px-4 py-3 text-foreground">{row.supply}</td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.terms}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Emission schedule
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Years from TGE
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Provider emission rate
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-background">
                {EMISSION.map((row) => (
                  <tr key={row.years}>
                    <td className="px-4 py-3 text-foreground">{row.years}</td>
                    <td className="px-4 py-3 text-foreground">{row.rate}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Hard-coded into the SPL emission program. No governance can
            override.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-4xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Provider lockup tiers
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Tier
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Schedule
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Rewards multiplier
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-background">
                {LOCKUP.map((row) => (
                  <tr key={row.tier}>
                    <td className="px-4 py-3 text-foreground">{row.tier}</td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.schedule}
                    </td>
                    <td className="px-4 py-3 text-foreground">
                      {row.multiplier}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Tier can be upgraded any time, never downgraded. Early-unlock is
            possible but carries a 50% penalty on the locked portion (burned),
            once per year, per provider.
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
