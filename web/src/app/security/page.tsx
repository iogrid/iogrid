import type { Metadata } from "next";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Security — iogrid",
  description:
    "Threat model, secrets handling, supply-chain attestation, runtime defense, and responsible-disclosure for the iogrid distributed mesh.",
};

// /security (Closes #464). Practitioner-facing description of how
// iogrid earns trust to relay residential bandwidth + run customer
// containers + ship signed binaries.

const PILLARS = [
  {
    h: "Identity + auth",
    body: "Every workload runs under a SPIFFE-style SVID minted per-namespace by identity-svc. JWTs are signed with an RSA-4096 keypair stored as a SealedSecret (not autogen — see #452 cutover). Refresh tokens are dual-key-rotated quarterly per docs/runbooks/jwt-keypair-rotation.md, so key rolls do NOT sign users out.",
  },
  {
    h: "mTLS everywhere",
    body: "Pod-to-pod traffic is mTLS-wrapped via Cilium WireGuard at the kernel layer. Cert rotation is hands-off (every 24h). Public surfaces (iogrid.org / api.iogrid.org / proxy.iogrid.org) use Let's Encrypt ECDSA via cert-manager DNS-01 challenges.",
  },
  {
    h: "Secrets",
    body: "OpenBao (vault-compatible, open-source) + external-secrets operator. No secret in git, ever. CI workflows mint short-lived tokens via the workflow GITHUB_TOKEN's packages:read scope; the cluster rotates its ghcr-pull credential every 45 min via .github/workflows/ghcr-pull-rotator.yml (Refs #454).",
  },
  {
    h: "Supply chain",
    body: "Every image is cosign-signed at push time + SBOM-attested (syft) + Trivy-scanned (admission gate rejects criticals). Pin-by-digest in every Deployment (no :latest, no :sha-... tags in prod). Image-pull blocks non-signed sources at admission.",
  },
  {
    h: "Runtime defense",
    body: "Kyverno admission policies (RuntimeDefault seccomp, runAsNonRoot, drop ALL capabilities, readOnlyRootFilesystem). Falco rules for syscall anomalies + crypto-miner signatures + container-escape attempts. Default-deny NetworkPolicies per namespace (gateway-system → svc layer only; cross-svc traffic explicitly allowed).",
  },
  {
    h: "Abuse layer",
    body: "Proxy traffic checked against NCMEC PhotoDNA, Google Safe Browsing, PhishTank, IWF, OFAC-sanctioned destinations BEFORE relay. Customers can layer additional deny lists per-workload. Providers see every byte they relayed in their /provide/audit feed (transparency-by-design).",
  },
  {
    h: "Observability",
    body: "Grafana Alloy + Loki + Mimir + Tempo. OTel traces follow every request from edge ingress through to provider. Audit events stream into NATS JetStream (per-byte ledger). Public transparency report at iogrid.org/transparency rolls quarterly.",
  },
];

const DISCLOSURE = [
  {
    h: "Responsible disclosure",
    body: "Email security@iogrid.org. PGP key fingerprint: published at iogrid.org/security. We acknowledge in 24h, triage in 72h, patch coordinated-disclosure within 90 days. Bounty: $500 (low) to $25k (RCE) per the published rubric.",
  },
  {
    h: "Out-of-scope",
    body: "Denial-of-service against public endpoints (we measure SLA + auto-scale; you don't need to demonstrate it). Self-XSS. Issues requiring physical access to a provider's hardware. Findings in third-party dependencies should be reported upstream first; we credit the chain.",
  },
  {
    h: "Last assessments",
    body: "Internal red-team: ongoing, monthly. Third-party pentest: scheduled Q3 2026 (Trail of Bits — confirmed). SOC 2 Type II: roadmapped Q1 2027 contingent on enterprise customer count. ISO 27001: roadmapped 2028.",
  },
];

export default function SecurityPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Security"
        title="How we earn the trust to relay residential bandwidth."
        subtitle="mTLS everywhere, default-deny NetworkPolicies, cosign-signed images, per-byte provider transparency, responsible-disclosure with PGP. Open-source bias on every layer."
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Seven pillars
          </h2>
          <dl className="mt-8 space-y-10">
            {PILLARS.map((p) => (
              <div key={p.h}>
                <dt className="text-base font-semibold text-foreground">
                  {p.h}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {p.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Responsible disclosure
          </h2>
          <dl className="mt-8 space-y-8">
            {DISCLOSURE.map((d) => (
              <div key={d.h}>
                <dt className="text-base font-semibold text-foreground">
                  {d.h}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {d.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </MarketingShell>
  );
}
