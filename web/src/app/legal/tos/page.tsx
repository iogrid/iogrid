import type { Metadata } from "next";
import Link from "next/link";
import { LegalPage } from "@/components/marketing/legal-page";

export const metadata: Metadata = {
  title: "Terms of service (placeholder)",
  description:
    "iogrid customer terms of service. Placeholder pending counsel drafting.",
};

/**
 * Terms of service — folded from marketing/app/legal/tos/page.tsx
 * into web/'s design system during EPIC #422 Phase 3. Content
 * preserved verbatim.
 */
export default function ToSPage() {
  return (
    <LegalPage title="Terms of service" lastUpdated="pending">
      <h2>1. The agreement</h2>
      <p>
        These terms govern your use of iogrid&rsquo;s services as a customer.
        By creating an account or submitting a workload, you agree to them.
      </p>

      <h2>2. Acceptable use</h2>
      <p>
        All customer traffic is subject to iogrid&rsquo;s Acceptable Use
        Policy, available at{" "}
        <Link href="/legal/aup">/legal/aup</Link>. Workloads inconsistent with
        the AUP will be terminated and your account may be suspended.
      </p>

      <h2>3. Pricing &amp; billing</h2>
      <p>
        Posted prices apply at the time a workload is dispatched. Invoices are
        issued monthly and payable in USD via Stripe, USDC on-chain, or $GRID
        with an applicable discount. Pre-paid credits do not expire.
      </p>

      <h2>4. Provider relationship</h2>
      <p>
        iogrid routes your workloads to providers who have opted into your
        workload&rsquo;s category. We are not the operator of the
        provider&rsquo;s hardware; we are the routing layer between you and
        providers. We disclose the categorical breakdown of routing in real
        time in your audit log.
      </p>

      <h2>5. Liability</h2>
      <p>
        iogrid&rsquo;s aggregate liability is limited to the amount paid in
        the trailing 12 months. We do not warrant uninterrupted service during
        Phase 1; Phase 2 ships explicit SLAs.
      </p>

      <h2>6. Governing law</h2>
      <p>
        These terms are governed by the laws of the jurisdiction in which the
        iogrid operating entity is incorporated (to be finalized at launch).
        Disputes are resolved by binding arbitration where permitted.
      </p>
    </LegalPage>
  );
}
