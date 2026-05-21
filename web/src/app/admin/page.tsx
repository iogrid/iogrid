import { PortalShell } from "@/components/layout/portal-shell";
import { ADMIN_NAV } from "@/app/admin/nav";

export const metadata = { title: "Admin — iogrid" };

/**
 * /admin — staff console root. Gated by middleware (auth) and by the
 * BFF's RequireRole("ADMIN") on every sub-call. Server Component; only
 * the per-sub-route panels need client interactivity.
 */
export default function AdminOverviewPage() {
  return (
    <PortalShell
      badge="Admin"
      title="Staff console"
      subtitle="Abuse review, KYC, provider audits, billing operations."
      nav={ADMIN_NAV}
      activeHref="/admin"
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Tile
          href="/admin/abuse"
          title="Abuse queue"
          description="Yellow-flagged events awaiting reviewer decision."
        />
        <Tile
          href="/admin/customers"
          title="Customers"
          description="KYC submissions, sanctions screening, business verification."
        />
        <Tile
          href="/admin/providers"
          title="Providers"
          description="Audit any provider's transparency feed for compliance."
        />
      </div>
      <p className="mt-6 text-xs text-zinc-500">
        Access to these pages is gated by the <code>is_admin</code> JWT claim
        — even with the route open in your browser, every action is
        independently authorised by the gateway-bff RequireRole middleware.
      </p>
    </PortalShell>
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
