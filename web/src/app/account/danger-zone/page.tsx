import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { DangerZonePanel } from "./panel";

export const metadata = { title: "Danger zone — iogrid" };

/**
 * /account/danger-zone — irrevocable actions guarded by a typed
 * confirmation. Today this means account deletion; in the future we'll
 * also house "export all data" and "transfer ownership" here.
 */
export default function AccountDangerZonePage() {
  return (
    <PortalShell
      badge="Account"
      title="Danger zone"
      subtitle="Irrevocable operations. Read the warning and type to confirm."
      nav={ACCOUNT_NAV}
      activeHref="/account/danger-zone"
    >
      <DangerZonePanel />
    </PortalShell>
  );
}
