import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/nav";
import { AbusePanel } from "./panel";

export const metadata = { title: "Abuse queue — iogrid admin" };

/**
 * /abuse — antiabuse-svc filter ruleset + (once the proto grows a
 * queue RPC) the pending review queue. Today we render the live
 * rules so on-call can verify what's currently being blocked.
 *
 * Moved out of web/src/app/admin/abuse/ in EPIC #422 Phase 1.
 */
export default function AdminAbusePage() {
  return (
    <AdminShell
      badge="Admin"
      title="Abuse queue"
      subtitle="Live filter ruleset + pending review queue."
      nav={ADMIN_NAV}
      activeHref="/abuse"
    >
      <AbusePanel />
    </AdminShell>
  );
}
