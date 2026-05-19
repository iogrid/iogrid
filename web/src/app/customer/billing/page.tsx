import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { BillingPanel } from "./panel";

export const metadata = { title: "Billing — iogrid" };

/**
 * /customer/billing — links into the Stripe Customer Portal and shows
 * the current subscription tier + spend-to-date for the in-flight
 * invoice. The portal is hosted by Stripe so we just redirect.
 */
export default function CustomerBillingPage() {
  return (
    <PortalShell
      badge="Customer"
      title="Billing"
      subtitle="Subscription, invoices, payment method."
      nav={CUSTOMER_NAV}
      activeHref="/customer/billing"
    >
      <BillingPanel />
    </PortalShell>
  );
}
