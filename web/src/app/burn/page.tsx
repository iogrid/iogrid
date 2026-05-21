import Link from "next/link";
import { BurnDashboard } from "./dashboard";
import { ThemeToggle } from "@/components/theme-toggle";

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
    <div className="min-h-screen bg-muted text-foreground dark:bg-background dark:text-foreground">
      <header className="border-b border-border bg-card dark:border-border">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-6 py-3">
          <Link
            href="/"
            className="text-lg font-bold tracking-tight"
            aria-label="iogrid home"
          >
            iogrid
          </Link>
          <div className="flex items-center gap-4">
            <p className="text-xs uppercase tracking-wide text-muted-foreground">
              Public burn dashboard
            </p>
            <ThemeToggle />
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-5xl px-6 py-8">
        <h1 className="text-3xl font-bold tracking-tight">$GRID burn ledger</h1>
        <p className="mt-2 max-w-2xl text-sm text-muted-foreground dark:text-muted-foreground">
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
