import type { Metadata } from "next";
import { LegalPage } from "@/components/marketing/legal-page";

export const metadata: Metadata = {
  title: "Privacy policy (placeholder)",
  description: "iogrid privacy policy. Placeholder pending counsel drafting.",
};

/**
 * Privacy policy — folded from marketing/app/legal/privacy/page.tsx
 * into web/'s design system during EPIC #422 Phase 3. Content
 * preserved verbatim.
 */
export default function PrivacyPage() {
  return (
    <LegalPage title="Privacy policy" lastUpdated="pending">
      <h2>What we collect</h2>
      <p>
        Account email, payout method, and usage metrics required to bill
        customers and pay providers. Provider hardware identifiers (CPU, GPU,
        OS) are collected for capability matching. Customer workload
        destinations and category labels are collected for audit logging.
      </p>

      <h2>What we do not collect</h2>
      <p>
        We do not see the plaintext payload of customer HTTPS traffic. We do
        not collect the contents of provider hardware (other than metrics they
        explicitly opt to share). We do not run third-party analytics on the
        provider dashboard or customer console.
      </p>

      <h2>How long we keep it</h2>
      <p>
        Account data is retained while the account is active and for 90 days
        after closure. Audit logs are retained for 12 months for customer
        compliance needs, and providers can purge their copy after 30 days.
      </p>

      <h2>Your rights</h2>
      <p>
        Users in the EU, UK, California, and other comparable jurisdictions
        can request a data export or deletion from their account settings. We
        honor these within 30 days.
      </p>

      <h2>Subprocessors</h2>
      <p>
        We use Stripe (payments), Stalwart SMTP (email), Hetzner Object
        Storage (artifact + audit log archive), Cloudflare (CDN), and the
        applicable on-chain primitives for $GRID settlement. A full
        subprocessor list will be published before paid launch.
      </p>
    </LegalPage>
  );
}
