import type { Metadata } from "next";
import { LegalPage } from "@/components/marketing/legal-page";

export const metadata: Metadata = {
  title: "Acceptable use policy (placeholder)",
  description:
    "iogrid acceptable use policy. Placeholder pending counsel drafting.",
};

/**
 * Acceptable Use Policy — folded from marketing/app/legal/aup/page.tsx
 * into web/'s design system during EPIC #422 Phase 3. Content
 * preserved verbatim.
 */
export default function AUPPage() {
  return (
    <LegalPage title="Acceptable use policy" lastUpdated="pending">
      <h2>What is allowed</h2>
      <p>
        Lawful B2B workloads: e-commerce price monitoring, SEO research,
        advertising verification, AI training data collection from the public
        web, threat intelligence research, brand-protection scanning,
        travel-fare aggregation, and similar use cases. iOS / macOS build CI
        on customer-owned source code. Docker workloads, GPU inference, and
        fine-tuning on customer-owned data and models.
      </p>

      <h2>What is forbidden</h2>
      <ul className="list-disc space-y-2 pl-6">
        <li>
          Anything involving CSAM, in any form. Hashes are filtered at the
          gateway and on the provider daemon.
        </li>
        <li>Phishing, credential-harvesting, or social-engineering payloads.</li>
        <li>
          Fraud, including account takeover, payment fraud, or coupon abuse.
        </li>
        <li>Targeted attacks against individuals (doxxing, stalkerware).</li>
        <li>DDoS or other availability attacks.</li>
        <li>
          Bypassing CAPTCHA or similar challenges in ways that violate the
          target site&rsquo;s ToS for malicious purposes.
        </li>
        <li>Traffic to or from sanctioned countries or entities.</li>
        <li>
          Cryptocurrency mining via Docker or GPU workloads &mdash;
          economically unsound at our price points and disruptive to the
          provider experience.
        </li>
        <li>
          Anything inconsistent with the categories a provider has opted into.
        </li>
      </ul>

      <h2>How we enforce it</h2>
      <p>
        Pre-flight filtering at the gateway and on the provider daemon blocks
        the most common abuse vectors before bytes are relayed. Behavioral
        anomaly detection flags accounts whose pattern (request rate,
        destination diversity, error ratios) drifts from their declared
        category. Confirmed abuse triggers account suspension, refund
        forfeiture, and where appropriate cooperation with law enforcement.
      </p>

      <h2>Provider protections</h2>
      <p>
        If a customer&rsquo;s use case causes legal exposure to a provider
        whose IP routed the traffic, iogrid&rsquo;s legal defense fund covers
        reasonable provider costs. Providers can revoke consent for any
        category at any time without notice; we route subsequent requests
        elsewhere.
      </p>
    </LegalPage>
  );
}
