import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "Acceptable use policy (placeholder)",
  description: "iogrid acceptable use policy. Placeholder pending counsel drafting.",
};

export default function AUPPage() {
  return (
    <article className="container-page py-16">
      <div className="mx-auto max-w-3xl">
        <span className="pill bg-warning/20 text-amber-700">Placeholder</span>
        <h1 className="mt-4 text-4xl font-extrabold tracking-tight text-neutral-900 md:text-5xl">
          Acceptable use policy
        </h1>
        <p className="mt-4 text-sm text-neutral-500">
          Last updated: pending — final language will be drafted by qualified
          counsel before Phase 1 launch.
        </p>

        <section className="mt-12 space-y-6 text-neutral-700">
          <h2 className="h-section text-neutral-900">What is allowed</h2>
          <p>
            Lawful B2B workloads: e-commerce price monitoring, SEO research,
            advertising verification, AI training data collection from the
            public web, threat intelligence research, brand-protection scanning,
            travel-fare aggregation, and similar use cases. iOS / macOS build
            CI on customer-owned source code. Docker workloads, GPU inference,
            and fine-tuning on customer-owned data and models.
          </p>

          <h2 className="h-section text-neutral-900">What is forbidden</h2>
          <ul className="list-disc space-y-2 pl-6">
            <li>Anything involving CSAM, in any form. Hashes are filtered at the gateway and on the provider daemon.</li>
            <li>Phishing, credential-harvesting, or social-engineering payloads.</li>
            <li>Fraud, including account takeover, payment fraud, or coupon abuse.</li>
            <li>Targeted attacks against individuals (doxxing, stalkerware).</li>
            <li>DDoS or other availability attacks.</li>
            <li>Bypassing CAPTCHA or similar challenges in ways that violate the target site&rsquo;s ToS for malicious purposes.</li>
            <li>Traffic to or from sanctioned countries or entities.</li>
            <li>Cryptocurrency mining via Docker or GPU workloads — economically unsound at our price points and disruptive to the provider experience.</li>
            <li>Anything inconsistent with the categories a provider has opted into.</li>
          </ul>

          <h2 className="h-section text-neutral-900">How we enforce it</h2>
          <p>
            Pre-flight filtering at the gateway and on the provider daemon
            blocks the most common abuse vectors before bytes are relayed.
            Behavioral anomaly detection flags accounts whose pattern
            (request rate, destination diversity, error ratios) drifts from
            their declared category. Confirmed abuse triggers account
            suspension, refund forfeiture, and where appropriate cooperation
            with law enforcement.
          </p>

          <h2 className="h-section text-neutral-900">Provider protections</h2>
          <p>
            If a customer&rsquo;s use case causes legal exposure to a provider
            whose IP routed the traffic, iogrid&rsquo;s legal defense fund
            covers reasonable provider costs. Providers can revoke consent
            for any category at any time without notice; we route subsequent
            requests elsewhere.
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
