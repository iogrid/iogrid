import Link from "next/link";
import { BurnDashboard } from "./dashboard";

export const metadata = {
  title: "$GRID burn dashboard — iogrid",
  description:
    "Public on-chain dashboard of every $GRID token burned. Updated continuously from the Solana mainnet incinerator account.",
};

/**
 * /burn — anonymous-readable public dashboard. Renders the total burn
 * counter, daily time-series, and a recent-events list. Anchored at
 * its own route (NOT inside /provide or /customer) so it can be
 * shared as a public link without requiring sign-in.
 *
 * The chrome is intentionally minimal — no PortalShell here because
 * /burn is reachable without an authenticated session.
 */
export default function BurnPage() {
  return (
    <div className="min-h-screen bg-zinc-50 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-50">
      <header className="border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-3">
          <Link
            href="/"
            className="text-lg font-bold tracking-tight"
            aria-label="iogrid home"
          >
            iogrid
          </Link>
          <p className="text-xs uppercase tracking-wide text-zinc-500">
            Public burn dashboard
          </p>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-3xl font-bold tracking-tight">$GRID burn ledger</h1>
        <p className="mt-2 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
          Every $GRID burn event is visible on-chain at the Solana
          incinerator address. iogrid permanently removes tokens via
          buyback-and-burn (2% of revenue), early-unlock penalties
          (50% of principal), and protocol-level emissions decay. The
          dashboard below mirrors that on-chain log.
        </p>

        <div className="mt-8">
          <BurnDashboard />
        </div>
      </div>
    </div>
  );
}
