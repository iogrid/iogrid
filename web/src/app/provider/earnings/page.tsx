import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDER_NAV } from "@/app/provider/nav";
import { EarningsView } from "./view";

export const metadata = {
  title: "Earnings — iogrid",
};

/**
 * /provider/earnings — daily / weekly / monthly view. Headline number,
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
      nav={PROVIDER_NAV}
      activeHref="/provider/earnings"
    >
      <EarningsView />
    </PortalShell>
  );
}
