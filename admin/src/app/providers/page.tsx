import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/nav";
import { ProviderAuditLookup } from "./lookup";
import { ProviderList } from "./provider-list";

export const metadata = { title: "Providers — iogrid admin" };

/**
 * /providers — list every paired provider in the pool + audit any
 * one of them via the transparency feed. The list calls providers-svc
 * .ListProviders directly via Traefik IngressRoute; the audit reuses
 * /api/v1/providers/audit/stream through the BFF.
 *
 * Moved out of web/src/app/admin/providers/ in EPIC #422 Phase 1.
 * Path dropped its `/admin` prefix because this entire app IS the
 * admin console (admin.iogrid.org).
 */
export default function AdminProvidersPage() {
  return (
    <AdminShell
      badge="Admin"
      title="Provider pool"
      subtitle="Every paired iogrid daemon, plus per-provider audit lookup."
      nav={ADMIN_NAV}
      activeHref="/providers"
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
    </AdminShell>
  );
}
