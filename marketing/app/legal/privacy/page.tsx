import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Privacy policy (placeholder)",
  description: "iogrid privacy policy. Placeholder pending counsel drafting.",
};

export default function PrivacyPage() {
  return (
    <article className="container-page py-16">
      <div className="mx-auto max-w-3xl">
        <span className="pill bg-warning/20 text-amber-700">Placeholder</span>
        <h1 className="mt-4 text-4xl font-extrabold tracking-tight text-neutral-900 md:text-5xl">
          Privacy policy
        </h1>
        <p className="mt-4 text-sm text-neutral-500">
          Last updated: pending — final language will be drafted by qualified
          counsel before Phase 1 launch.
        </p>

        <section className="mt-12 space-y-6 text-neutral-700">
          <h2 className="h-section text-neutral-900">What we collect</h2>
          <p>
            Account email, payout method, and usage metrics required to bill
            customers and pay providers. Provider hardware identifiers (CPU,
            GPU, OS) are collected for capability matching. Customer workload
            destinations and category labels are collected for audit logging.
          </p>

          <h2 className="h-section text-neutral-900">What we do not collect</h2>
          <p>
            We do not see the plaintext payload of customer HTTPS traffic.
            We do not collect the contents of provider hardware (other than
            metrics they explicitly opt to share). We do not run third-party
            analytics on the provider dashboard or customer console.
          </p>

          <h2 className="h-section text-neutral-900">How long we keep it</h2>
          <p>
            Account data is retained while the account is active and for 90
            days after closure. Audit logs are retained for 12 months for
            customer compliance needs, and providers can purge their copy
            after 30 days.
          </p>

          <h2 className="h-section text-neutral-900">Your rights</h2>
          <p>
            Users in the EU, UK, California, and other comparable
            jurisdictions can request a data export or deletion from their
            account settings. We honor these within 30 days.
          </p>

          <h2 className="h-section text-neutral-900">Subprocessors</h2>
          <p>
            We use Stripe (payments), Stalwart SMTP (email), Hetzner Object
            Storage (artifact + audit log archive), Cloudflare (CDN), and the
            applicable on-chain primitives for $GRID settlement. A full
            subprocessor list will be published before paid launch.
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
