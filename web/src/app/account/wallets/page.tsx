import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { WalletsView } from "./view";

export const metadata = { title: "Wallets — iogrid" };

/**
 * /account/wallets — manages the Solana wallets bound to the user's
 * identity. Providers MUST bind a wallet before they can receive
 * $GRID payouts (see docs/TOKENOMICS.md). Customers may optionally
 * bind to pay in $GRID for the 20% discount.
 *
 * The page is a thin shell that delegates to a client component for
 * the wallet-adapter interactivity (signMessage, connection state).
 */
export default function WalletsPage() {
  return (
    <PortalShell
      badge="Account"
      title="Wallets"
      subtitle="Bind one or more Solana wallets to your iogrid identity. Providers receive $GRID payouts to a bound wallet; customers paying in $GRID get a 20% discount."
      nav={ACCOUNT_NAV}
      activeHref="/account/wallets"
    >
      <WalletsView />
    </PortalShell>
  );
}
