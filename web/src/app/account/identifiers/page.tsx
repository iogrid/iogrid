import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { IdentifiersPanel } from "./panel";

export const metadata = { title: "Identifiers — iogrid" };

/**
 * /account/identifiers — show all bound identifiers (emails + OAuth
 * providers) and let the user add / remove them. Backed by
 * identity-svc through /api/v1/me.
 */
export default function AccountIdentifiersPage() {
  return (
    <PortalShell
      badge="Account"
      title="Identifiers"
      subtitle="Emails and OAuth providers that can sign you in to this account."
      nav={ACCOUNT_NAV}
      activeHref="/account/identifiers"
    >
      <IdentifiersPanel />
    </PortalShell>
  );
}
