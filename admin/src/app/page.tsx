import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/(authed)/nav";

export const metadata = { title: "iogrid admin" };

/**
 * / — staff console root. Gated by middleware (auth + IOGRID_ADMIN_EMAILS
 * allowlist) and by the BFF's RequireRole("ADMIN") on every sub-call.
 * Server Component; only the per-sub-route panels need client
 * interactivity.
 */
export default function AdminOverviewPage() {
  return (
    <AdminShell
      badge="Admin"
      title="Staff console"
      subtitle="Abuse review, KYC, provider audits, billing operations."
      nav={ADMIN_NAV}
      activeHref="/"
    >
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Tile
          href="/abuse"
          title="Abuse queue"
          description="Yellow-flagged events awaiting reviewer decision."
        />
        <Tile
          href="/customers"
          title="Customers"
          description="KYC submissions, sanctions screening, business verification."
        />
        <Tile
          href="/providers"
          title="Providers"
          description="Audit any provider's transparency feed for compliance."
        />
        <Tile
          href="/finops"
          title="Finops"
          description="Off-ramp sweeps, payout batches, ledger reconciliation."
        />
        <Tile
          href="/settings"
          title="Settings"
          description="Operator preferences and per-admin configuration."
        />
      </div>
      <p className="mt-6 text-xs text-zinc-500">
        Access to these pages is gated by the <code>IOGRID_ADMIN_EMAILS</code>{" "}
        allowlist (edge middleware) and by the gateway-bff{" "}
        <code>RequireRole(&quot;ADMIN&quot;)</code> middleware on every
        upstream call.
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
