import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { CustomerOverview } from "./overview";

export const metadata = {
  title: "Customer dashboard — iogrid",
};

/**
 * /customer — workspace at-a-glance: spend this month, running
 * workloads, recent dispatches, links into sub-routes.
 */
export default function CustomerDashboardPage() {
  return (
    <PortalShell
      badge="Customer"
      title="Workspace"
      subtitle="Submit workloads, monitor scheduling, manage API keys and billing."
      nav={CUSTOMER_NAV}
      activeHref="/customer"
    >
      <CustomerOverview />
    </PortalShell>
  );
}
