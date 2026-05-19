import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { WorkloadsPanel } from "./panel";

export const metadata = { title: "Workloads — iogrid" };

/**
 * /customer/workloads — submit a new workload + see recent dispatches.
 * The form mirrors the workloads.v1.SubmitWorkloadRequest contract; the
 * full power-user surface (workload graphs, dispatch retries) lands in
 * a follow-up PR.
 */
export default function CustomerWorkloadsPage() {
  return (
    <PortalShell
      badge="Customer"
      title="Workloads"
      subtitle="Submit jobs to the mesh and watch them dispatch in real time."
      nav={CUSTOMER_NAV}
      activeHref="/customer/workloads"
    >
      <WorkloadsPanel />
    </PortalShell>
  );
}
