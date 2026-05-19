import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { UpdatesPanel } from "./panel";

export const metadata = { title: "Updates — iogrid" };

/**
 * /account/updates — provider-facing controls for the daemon auto-update
 * worker (issue #59). Lets the operator:
 *
 *  - pick a release channel (stable / beta / canary)
 *  - flip auto-update on/off
 *  - fire a manual "Check now" against the manifest server
 *  - inspect the rolling history (last 50 checks + outcomes)
 *
 * Backed by gateway-bff routes that proxy to the local daemon's
 * /api/v1/account/updates endpoint. In Phase 0 the BFF returns a
 * disabled-by-default stub when the daemon hasn't paired yet.
 */
export default function AccountUpdatesPage() {
  return (
    <PortalShell
      badge="Account"
      title="Updates"
      subtitle="Keep your iogrid daemon up to date — securely, automatically, with one-step rollback."
      nav={ACCOUNT_NAV}
      activeHref="/account/updates"
    >
      <UpdatesPanel />
    </PortalShell>
  );
}
