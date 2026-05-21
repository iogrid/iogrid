import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/nav";

export const metadata = { title: "Overview — iogrid admin" };

/**
 * / — admin console overview (EPIC #422 Phase 1).
 *
 * Replaces the legacy `/admin` route that used to live in web/.
 * Since this entire app IS the admin console, the canonical path
 * is now just `/` (no `/admin` prefix needed — admin.iogrid.org/
 * already names the surface).
 *
 * Gated by middleware (auth + IOGRID_ADMIN_EMAILS allowlist) and by
 * the BFF's RequireRole("ADMIN") on every sub-call. Server Component;
 * only the per-sub-route panels need client interactivity.
 */
export default function AdminOverviewPage() {
  return (
    <AdminShell
      badge="Admin"
      title="Staff console"
      subtitle="Provider pool, abuse review, billing audit, system health."
      nav={ADMIN_NAV}
      activeHref="/"
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Tile
          href="/providers"
          title="Providers"
          description="Every paired iogrid daemon + per-provider audit lookup."
        />
        <Tile
          href="/abuse"
          title="Abuse queue"
          description="Yellow-flagged events awaiting reviewer decision."
        />
        <Tile
          href="/billing"
          title="Billing"
          description="Customer KYC, sanctions screening, payout audit."
        />
        <Tile
          href="/health"
          title="System health"
          description="Cluster health, control-plane SLOs, deployment status."
        />
      </div>
      <p className="mt-6 text-xs text-zinc-500">
        Access to these pages is gated by the <code>is_admin</code> claim and
        the <code>IOGRID_ADMIN_EMAILS</code> allowlist — every action is
        independently authorised by gateway-bff&apos;s RequireRole middleware.
      </p>
    </AdminShell>
  );
}

function Tile({
  href,
  title,
  description,
}: {
  href: string;
  title: string;
  description: string;
}) {
  return (
    <a
      href={href}
      className="rounded-md border border-zinc-200 bg-white p-4 transition-colors hover:border-zinc-400 dark:border-zinc-800 dark:bg-zinc-900"
    >
      <p className="text-sm font-medium">{title}</p>
      <p className="mt-1 text-xs text-zinc-500">{description}</p>
    </a>
  );
}
