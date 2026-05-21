import { PortalShell } from "@/components/layout/portal-shell";
import { ADMIN_NAV } from "@/app/admin/nav";

export const metadata = { title: "Customers — iogrid admin" };

/**
 * /admin/customers — KYC review surface. Until billing-svc grows the
 * /kyc/review RPC, render the structured placeholder so on-call has a
 * stable URL to bookmark.
 */
export default function AdminCustomersPage() {
  return (
    <PortalShell
      badge="Admin"
      title="Customer KYC"
      subtitle="Business verification + sanctions screening."
      nav={ADMIN_NAV}
      activeHref="/admin/customers"
    >
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">
        KYC review queue is gated behind the billing-svc <code>/kyc/review</code>
        {" "}RPC (tracked in #42). Until that ships, reviewers should pull the
        nightly export from <code>billing-svc</code>: it lands in S3 every
        00:30 UTC at <code>s3://iogrid-ops/kyc-review/YYYY-MM-DD.jsonl</code>.
      </div>
    </PortalShell>
  );
}
