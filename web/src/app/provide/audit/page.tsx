import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDE_NAV } from "@/app/provide/nav";
import { AuditFeed } from "./feed";

export const metadata = {
  title: "Transparency feed — iogrid",
};

/**
 * /provide/audit — the killer feature. Renders the live SSE feed of
 * what the provider's machine is doing right now (which categories,
 * customers, destinations, bytes). Every row exposes three one-click
 * block controls.
 */
export default function ProvideAuditPage() {
  return (
    <PortalShell
      badge="Provider"
      title="Transparency feed"
      subtitle="Every workload your machine is handling, in real time. Block any row in one click — the change ships to the daemon within seconds."
      nav={PROVIDE_NAV}
      activeHref="/provide/audit"
    >
      <AuditFeed />
    </PortalShell>
  );
}
