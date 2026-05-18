import type { Metadata } from "next";
import Link from "next/link";
import { Hero } from "@/components/Hero";
import { FeatureGrid } from "@/components/FeatureGrid";

export const metadata: Metadata = {
  title: "$GRID — the network's unit of work",
  description:
    "$GRID is iogrid's utility token. It pays providers, buys services at 20% off, and votes on routing parameters. We do not promise price appreciation.",
};

export default function TokenPage() {
  return (
    <>
      <Hero
        eyebrow="$GRID"
        title="A unit of work, not a speculation."
        subtitle={
          <>
            $GRID is the optional native currency of the iogrid mesh. Providers
            can elect to earn in $GRID. Customers can elect to pay in $GRID for a
            20% discount. Holders can vote on routing parameters. That&rsquo;s
            the whole story. We do not — and will never — promise that the price
            goes up.
          </>
        }
        primaryCta={{ href: "#utility", label: "What $GRID does" }}
      />

      <section id="utility" className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">Utility, three ways</h2>
          <ol className="mt-6 space-y-4 text-neutral-700">
            <li>
              <strong className="text-neutral-900">1. Payout currency.</strong>{" "}
              Providers may elect $GRID as their payout currency at signup. The
              network mints rewards continuously per workload contributed. Cash
              and free-VPN payouts remain fully supported — $GRID is opt-in.
            </li>
            <li>
              <strong className="text-neutral-900">2. Customer payment.</strong>{" "}
              Customers paying invoices in $GRID receive a 20% discount vs
              list-price USD. The discount exists because paying in $GRID
              removes one swap step from our settlement flow.
            </li>
            <li>
              <strong className="text-neutral-900">3. Governance signal.</strong>{" "}
              Long-term holders vote on routing parameters: regional pool sizes,
              category opt-out defaults, anti-abuse thresholds. One token,
              one vote. Not retroactive: parameter changes apply forward.
            </li>
          </ol>
          <p className="mt-6 text-sm text-neutral-500">
            What $GRID is <em>not</em>: a security, an investment contract, or a
            promise of future value. The
            {" "}
            <Link href="/docs" className="underline">whitepaper</Link>
            {" "}
            documents every economic mechanism. Read it before holding.
          </p>
        </div>
      </section>

      <FeatureGrid
        title="Mechanics, plainly"
        features={[
          {
            title: "Solana SPL token",
            body: "Sub-second finality. ~$0.0005 per transaction. Audited Token-2022 program. Multisig treasury via Squads Protocol.",
          },
          {
            title: "Provider vesting",
            body: "Earned $GRID vests over 30/90 days with longer-lockup tiers available at signup for higher reward multipliers. Stops day-1 dumps; rewards conviction.",
          },
          {
            title: "Revenue burn",
            body: "2% of all customer revenue is converted to $GRID via Jupiter swap and burned. Public, on-chain, verifiable.",
          },
          {
            title: "Emission halving",
            body: "Bitcoin-style halving every 24 months. Year-1 emission is 5% of supply; Year-10 cumulative is ~48.5%.",
          },
          {
            title: "Customer discount",
            body: "Pay invoices in $GRID and the gateway applies a 20% discount. The tokens flow through to providers + the burn wallet.",
          },
          {
            title: "Geo-restrictions at launch",
            body: "US persons are excluded from primary issuance. Standard practice for Solana-ecosystem tokens. See the legal section of the whitepaper.",
          },
        ]}
      />

      <section className="container-page py-16">
        <div className="mx-auto max-w-3xl">
          <h2 className="h-section text-neutral-900">What we do not say</h2>
          <ul className="mt-6 space-y-3 text-neutral-700">
            <li>
              We do <strong>not</strong> say &ldquo;buy $GRID, the price will
              rise.&rdquo;
            </li>
            <li>
              We do <strong>not</strong> say &ldquo;hold $GRID for passive
              yield.&rdquo;
            </li>
            <li>
              We do <strong>not</strong> publish forward-looking price targets.
            </li>
            <li>
              We do <strong>not</strong> run influencer campaigns or pump
              channels.
            </li>
            <li>
              We do <strong>not</strong> pay for shilling, &ldquo;market
              making,&rdquo; or wash trading.
            </li>
          </ul>
          <p className="mt-6 text-neutral-700">
            $GRID exists because deflationary, network-native settlement
            removes friction from the provider-payout flow. The price will be
            whatever the open market decides. Past performance of similar
            tokens is not indicative of future results. If you&rsquo;re not
            already comfortable with that, just take cash payouts — the network
            works fine without you owning a single $GRID.
          </p>
        </div>
      </section>

      <section className="container-page py-16">
        <div className="rounded-2xl bg-neutral-900 px-8 py-12 text-center text-neutral-100 md:py-16">
          <h2 className="h-section text-white">Read before you hold</h2>
          <p className="mx-auto mt-4 max-w-2xl text-lg text-neutral-300">
            The full $GRID whitepaper documents emission, vesting, governance,
            and the legal risks we believe are material. It will be published
            ahead of mainnet TGE.
          </p>
          <Link
            href="/docs"
            className="btn-primary mt-8 bg-white text-primary-700 hover:bg-neutral-100"
          >
            Whitepaper (pre-TGE draft)
          </Link>
        </div>
      </section>
    </>
  );
}
