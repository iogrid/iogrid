import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { BillingPanel } from "./panel";

export const metadata = { title: "Billing — iogrid" };

/**
 * /customer/billing — prepaid $GRID balance surface (#632). Shows the
 * current spendable balance, a top-up CTA, the grace-overage cap + any
 * amount owed, and bandwidth-consumption context. Prepaid + capped-grace
 * money model (founder-ruled) — no Stripe subscription tiers here.
 */
export default function CustomerBillingPage() {
  return (
    <PortalShell
      badge="Customer"
      title="Billing"
      subtitle="Prepaid $GRID balance, top-up, and grace overage."
      nav={CUSTOMER_NAV}
      activeHref="/customer/billing"
    >
      <BillingPanel />
    </PortalShell>
  );
}
