import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/nav";

export const metadata = { title: "Billing — iogrid admin" };

/**
 * /billing — admin billing audit surface (EPIC #422 Phase 1 stub).
 *
 * Replaces the legacy /admin/customers page that used to live in web/
 * and was scoped narrowly to KYC review. Billing in the admin app
 * covers KYC, sanctions screening, AND payout audit — the full
 * back-office surface.
 *
 * The structured placeholder ships in Phase 1 so the nav is complete
 * (every tab in `ADMIN_NAV` resolves to a real page). The backing BFF
 * routes (billing-svc /kyc/review, /payouts/audit) get wired in a
 * follow-up PR once the proto RPCs land.
 */
export default function AdminBillingPage() {
  return (
    <AdminShell
      badge="Admin"
      title="Billing"
      subtitle="KYC review, sanctions screening, payout audit."
      nav={ADMIN_NAV}
      activeHref="/billing"
    >
      <div className="space-y-4">
        <div className="rounded-md border border-warning/20 bg-warning/10 p-4 text-sm text-warning/30 dark:border-warning/30 dark:bg-warning/15 dark:text-warning/20">
          <p className="font-medium">Backing RPCs not yet shipped.</p>
          <p className="mt-1 text-xs">
            Billing review surfaces are gated behind the billing-svc{" "}
            <code>/kyc/review</code> and <code>/payouts/audit</code> RPCs
            (tracked in #42 and #361 follow-ups). Until they ship,
            reviewers should pull the nightly export:
            <code className="ml-1">
              s3://iogrid-ops/kyc-review/YYYY-MM-DD.jsonl
            </code>
            .
          </p>
        </div>
        <div className="rounded-md border border-border p-4 text-sm dark:border-foreground">
          <h2 className="font-medium">What will land here</h2>
          <ul className="mt-2 list-inside list-disc space-y-1 text-xs text-foreground dark:text-muted-foreground">
            <li>
              KYC submissions queue — business verification + sanctions
              screening, paginated by submission age.
            </li>
            <li>
              Payout audit — per-provider $GRID payout ledger, currency
              breakdown, and dispute history.
            </li>
            <li>
              Refund + chargeback handler — customer-side billing
              disputes, linked back to the originating workload.
            </li>
          </ul>
        </div>
      </div>
    </AdminShell>
  );
}
