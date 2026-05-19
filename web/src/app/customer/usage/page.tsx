import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { UsageView } from "./view";

export const metadata = { title: "Usage — iogrid" };

/**
 * /customer/usage — bandwidth + compute usage charts. The data feeds
 * the monthly invoice on /customer/billing.
 */
export default function CustomerUsagePage() {
  return (
    <PortalShell
      badge="Customer"
      title="Usage"
      subtitle="Per-workload metering powering this month's invoice."
      nav={CUSTOMER_NAV}
      activeHref="/customer/usage"
    >
      <UsageView />
    </PortalShell>
  );
}
