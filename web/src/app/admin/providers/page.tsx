import { PortalShell } from "@/components/layout/portal-shell";
import { ADMIN_NAV } from "@/app/admin/nav";
import { ProviderAuditLookup } from "./lookup";
import { ProviderList } from "./provider-list";

export const metadata = { title: "Providers — iogrid admin" };

/**
 * /admin/providers — list every paired provider in the pool + audit any
 * one of them via the transparency feed. The list calls providers-svc
 * .ListProviders directly via Traefik IngressRoute; the audit reuses
 * /api/v1/provide/audit/stream through the BFF.
 */
export default function AdminProvidersPage() {
  return (
    <PortalShell
      badge="Admin"
      title="Provider pool"
      subtitle="Every paired iogrid daemon, plus per-provider audit lookup."
      nav={ADMIN_NAV}
      activeHref="/admin/providers"
    >
      <section className="space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
          Paired providers
        </h2>
        <ProviderList />
      </section>
      <section className="mt-8 space-y-3">
        <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
          Audit a provider
        </h2>
        <ProviderAuditLookup />
      </section>
    </PortalShell>
  );
}
