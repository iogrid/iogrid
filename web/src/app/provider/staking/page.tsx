import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDER_NAV } from "@/app/provider/nav";
import { StakingView } from "./view";

export const metadata = { title: "Staking — iogrid" };

/**
 * /provider/staking — provider-side staking dashboard. Open new
 * positions, see active locks, claim accrued yield, and (with a 50%
 * burn warning) early-unlock. Mechanics defined in
 * docs/TOKENOMICS.md §Layer-3 + §Optional bonus lockup tiers.
 */
export default function ProviderStakingPage() {
  return (
    <PortalShell
      badge="Provider"
      title="Staking"
      subtitle="Your $GRID earnings lockup, tier multiplier, and the Ping wallet your vested $GRID off-ramps to. Longer locks earn more — early-unlock burns 50% of principal."
      nav={PROVIDER_NAV}
      activeHref="/provider/staking"
    >
      <StakingView />
    </PortalShell>
  );
}
