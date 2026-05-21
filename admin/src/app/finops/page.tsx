import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/(authed)/nav";

export const metadata = { title: "Finops — iogrid admin" };

/**
 * /finops — financial operations surface. Placeholder until billing-svc
 * grows the off-ramp + payout-batch RPCs. The route exists today so
 * on-call has a stable URL to bookmark and so the admin nav doesn't
 * 404 against a tab that the operator can already see.
 */
export default function AdminFinopsPage() {
  return (
    <AdminShell
      badge="Admin"
      title="Finops"
      subtitle="Off-ramp sweeps, payout batches, ledger reconciliation."
      nav={ADMIN_NAV}
      activeHref="/finops"
    >
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">
        Finops dashboard pending the billing-svc{" "}
        <code>/finops/payouts</code> + <code>/finops/sweeps</code> RPCs.
        Until those ship, on-call should pull the daily ledger export from
        <code> billing-svc</code>: it lands in S3 every 01:00 UTC at{" "}
        <code>s3://iogrid-ops/finops/ledger/YYYY-MM-DD.jsonl</code>.
      </div>
    </AdminShell>
  );
}
