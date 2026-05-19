import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDE_NAV } from "@/app/provide/nav";
import { EarningsView } from "./view";

export const metadata = {
  title: "Earnings — iogrid",
};

/**
 * /provide/earnings — daily / weekly / monthly view. Headline number,
 * time-series chart, breakdown by workload type, and a payout-method
 * picker (Cash / Free VPN / Charity).
 */
export default function ProvideEarningsPage() {
  return (
    <PortalShell
      badge="Provider"
      title="Earnings"
      subtitle="Daily, weekly and monthly breakdowns. Pick how you want to be paid."
      nav={PROVIDE_NAV}
      activeHref="/provide/earnings"
    >
      <EarningsView />
    </PortalShell>
  );
}
