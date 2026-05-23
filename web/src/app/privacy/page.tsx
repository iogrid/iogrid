import type { Metadata } from "next";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Privacy notice — iogrid",
  description:
    "What data iogrid collects, why, where it goes, how long we keep it, and how to exercise your GDPR / CCPA rights.",
};

// /privacy (Closes #462). Plain-language privacy notice describing
// each data flow + the GDPR / CCPA / Turkish KVKK obligations attached.

const LAST_UPDATED = "2026-05-23";

const SECTIONS = [
  {
    h: "1. Who we are",
    body: "iogrid is operated by Openova (Cayman Islands Foundation Companies Law, registration in progress). Contact for any privacy question: privacy@iogrid.org. EU representative: TBD before EU traffic onboarding.",
  },
  {
    h: "2. What we collect — providers",
    body: "If you install the iogrid daemon: machine fingerprint (OS, arch, CPU/GPU model), IP address while paired, bandwidth byte counters per workload, geolocation (country + region only, derived from your public IP via MaxMind GeoIP2), payout address if you bind one ($GRID Solana wallet, Stripe Connect account, or charity destination). NOT collected: file contents, browsing history, account credentials for other services, microphone, camera, keystrokes.",
  },
  {
    h: "3. What we collect — customers",
    body: "Email address (for account creation), API keys you mint (hashed in DB), workload metadata (destination hostnames you proxy to, container image refs you run, build artifacts you produce), per-byte / per-second usage counters, payment-processor data (Stripe customer ID, Solana mint address for $GRID), workspace membership.",
  },
  {
    h: "4. What we do NOT do",
    body: "We do NOT sell or rent your data to third parties. We do NOT use your data to train AI models. We do NOT serve advertising. We do NOT correlate provider data with customer data (the matching layer routes traffic but the audit log never joins both sides for marketing purposes).",
  },
  {
    h: "5. Third-party processors",
    body: "Stripe (card payments, subscription billing — US, PCI-DSS), Solana on-chain ($GRID payouts — public ledger by design), GitHub (OAuth login, container registry), AWS (S3-compatible bucket for build artifacts — region: eu-central-1), MaxMind (GeoIP2 country lookup — no PII shared), Stalwart self-hosted (mail), NextAuth + JWT (session cookies — server-side only).",
  },
  {
    h: "6. Retention",
    body: "Account profile: retained until you delete it. Audit + transparency events: 90 days rolling. Invoice + tax records: 7 years (EU + Cayman tax law). Provider machine fingerprints: rotated quarterly. Backups: encrypted at rest, 30-day cold-storage TTL.",
  },
  {
    h: "7. Your rights (GDPR / CCPA / KVKK)",
    body: "You can request a copy of your data, deletion, correction, or processing-restriction at any time by emailing privacy@iogrid.org. Default response window: 30 days. The transparency feed shows your provider-side activity in real time; you can revoke any customer-side API key from /account/api-keys without contacting us.",
  },
  {
    h: "8. Security",
    body: "mTLS between every service. Secrets in OpenBao + external-secrets (no plaintext in the cluster, never in git). Container images cosign-signed + SBOM-published + Trivy-scanned at admission. Responsible-disclosure: security@iogrid.org; PGP key on docs.iogrid.org.",
  },
  {
    h: "9. Changes",
    body: `This notice was last updated on ${LAST_UPDATED}. Material changes get an email to every active account + a banner on /account for 30 days before they take effect.`,
  },
];

export default function PrivacyPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Legal"
        title="Privacy notice"
        body={`What data iogrid collects, why, where it goes, how long we keep it, and how to exercise your GDPR / CCPA / KVKK rights. Last updated ${LAST_UPDATED}.`}
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <dl className="space-y-10">
            {SECTIONS.map((s) => (
              <div key={s.h}>
                <dt className="text-base font-semibold text-foreground">
                  {s.h}
                </dt>
                <dd className="mt-3 text-sm leading-relaxed text-muted-foreground">
                  {s.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </MarketingShell>
  );
}
