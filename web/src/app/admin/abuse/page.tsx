import { PortalShell } from "@/components/layout/portal-shell";
import { ADMIN_NAV } from "@/app/admin/nav";
import { AbusePanel } from "./panel";

export const metadata = { title: "Abuse queue — iogrid admin" };

/**
 * /admin/abuse — antiabuse-svc filter ruleset + (once the proto grows
 * a queue RPC) the pending review queue. Today we render the live
 * rules so on-call can verify what's currently being blocked.
 */
export default function AdminAbusePage() {
  return (
    <PortalShell
      badge="Admin"
      title="Abuse queue"
      subtitle="Live filter ruleset + pending review queue."
      nav={ADMIN_NAV}
      activeHref="/admin/abuse"
    >
      <AbusePanel />
    </PortalShell>
  );
}
