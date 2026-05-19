import { PortalShell } from "@/components/layout/portal-shell";
import { CUSTOMER_NAV } from "@/app/customer/nav";
import { ApiKeysPanel } from "./panel";

export const metadata = { title: "API keys — iogrid" };

/**
 * /customer/api-keys — CRUD over the workspace's API keys.
 * Plaintext is only shown once (right after create), enforced by the
 * BFF. The UI mirrors that contract: a one-shot copy-to-clipboard
 * modal, then the key disappears.
 */
export default function CustomerApiKeysPage() {
  return (
    <PortalShell
      badge="Customer"
      title="API keys"
      subtitle="Service accounts that submit workloads on behalf of this workspace."
      nav={CUSTOMER_NAV}
      activeHref="/customer/api-keys"
    >
      <ApiKeysPanel />
    </PortalShell>
  );
}
