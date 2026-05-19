import { PortalShell } from "@/components/layout/portal-shell";
import { ADMIN_NAV } from "@/app/admin/nav";
import { ProviderAuditLookup } from "./lookup";

export const metadata = { title: "Providers — iogrid admin" };

/**
 * /admin/providers — audit any provider's transparency feed. Reuses
 * the same /api/v1/provide/audit/stream endpoint but with the
 * provider_id query param + the admin scope token.
 */
export default function AdminProvidersPage() {
  return (
    <PortalShell
      badge="Admin"
      title="Provider audits"
      subtitle="Inspect any provider's transparency feed for compliance review."
      nav={ADMIN_NAV}
      activeHref="/admin/providers"
    >
      <ProviderAuditLookup />
    </PortalShell>
  );
}
