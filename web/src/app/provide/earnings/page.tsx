import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDE_NAV } from "@/app/provide/nav";
import { EarningsView } from "./view";

export const metadata = {
  title: "Earnings — iogrid",
};

/**
 * /provide/earnings — daily / weekly / monthly view. Headline number,
 * time-series chart, breakdown by workload type, and a payout-method
 * picker (Hold $GRID default / Cash / Charity). Earnings accrue in
 * $GRID; cash + charity variants auto-swap via billing-svc's monthly
 * off-ramp cron.
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
