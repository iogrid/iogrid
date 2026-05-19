import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { SessionsPanel } from "./panel";

export const metadata = { title: "Sessions — iogrid" };

/**
 * /account/sessions — list active refresh-token-backed sessions. The
 * "current" pill is set on the row matching this browser's session id.
 */
export default function AccountSessionsPage() {
  return (
    <PortalShell
      badge="Account"
      title="Sessions"
      subtitle="Every browser, daemon and CI runner currently signed in to this account."
      nav={ACCOUNT_NAV}
      activeHref="/account/sessions"
    >
      <SessionsPanel />
    </PortalShell>
  );
}
