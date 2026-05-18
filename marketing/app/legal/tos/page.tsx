import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Terms of service (placeholder)",
  description: "iogrid customer terms of service. Placeholder pending counsel drafting.",
};

export default function ToSPage() {
  return (
    <article className="container-page py-16">
      <div className="mx-auto max-w-3xl">
        <span className="pill bg-warning/20 text-amber-700">Placeholder</span>
        <h1 className="mt-4 text-4xl font-extrabold tracking-tight text-neutral-900 md:text-5xl">
          Terms of service
        </h1>
        <p className="mt-4 text-sm text-neutral-500">
          Last updated: pending — final language will be drafted by qualified
          counsel before Phase 1 launch.
        </p>

        <section className="mt-12 space-y-6 text-neutral-700">
          <h2 className="h-section text-neutral-900">1. The agreement</h2>
          <p>
            These terms govern your use of iogrid&rsquo;s services as a
            customer. By creating an account or submitting a workload, you
            agree to them.
          </p>

          <h2 className="h-section text-neutral-900">2. Acceptable use</h2>
          <p>
            All customer traffic is subject to iogrid&rsquo;s Acceptable Use
            Policy, available at <a href="/legal/aup" className="text-primary-600 underline">/legal/aup</a>.
            Workloads inconsistent with the AUP will be terminated and your
            account may be suspended.
          </p>

          <h2 className="h-section text-neutral-900">3. Pricing &amp; billing</h2>
          <p>
            Posted prices apply at the time a workload is dispatched. Invoices
            are issued monthly and payable in USD via Stripe, USDC on-chain, or
            $GRID with an applicable discount. Pre-paid credits do not expire.
          </p>

          <h2 className="h-section text-neutral-900">4. Provider relationship</h2>
          <p>
            iogrid routes your workloads to providers who have opted into your
            workload&rsquo;s category. We are not the operator of the
            provider&rsquo;s hardware; we are the routing layer between you and
            providers. We disclose the categorical breakdown of routing in
            real time in your audit log.
          </p>

          <h2 className="h-section text-neutral-900">5. Liability</h2>
          <p>
            iogrid&rsquo;s aggregate liability is limited to the amount paid in
            the trailing 12 months. We do not warrant uninterrupted service
            during Phase 1; Phase 2 ships explicit SLAs.
          </p>

          <h2 className="h-section text-neutral-900">6. Governing law</h2>
          <p>
            These terms are governed by the laws of the jurisdiction in which
            the iogrid operating entity is incorporated (to be finalized at
            launch). Disputes are resolved by binding arbitration where
            permitted.
          </p>
        </section>

        <p className="mt-12 rounded-lg bg-neutral-50 p-4 text-sm text-neutral-500">
          This page is a public scaffold so the URL is reachable from
          navigation and the design is in place. The substantive legal text
          will be replaced ahead of paid traffic.
        </p>
      </div>
    </article>
  );
}
