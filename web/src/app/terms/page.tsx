import type { Metadata } from "next";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Terms of Service — iogrid",
  description:
    "Provider Terms of Service, Customer Terms of Service, and Acceptable Use Policy for the iogrid distributed compute + bandwidth mesh.",
};

// /terms (Closes #463). Wraps the three legal documents the platform
// runs on. Source content seeded from legal/{provider,customer}-tos.md
// + the abuse policy from antiabuse-svc operational docs.

const LAST_UPDATED = "2026-05-23";

const PROVIDER_TOS = [
  {
    h: "P1. Eligibility",
    body: "You can pair the iogrid daemon if you are 18+, legally responsible for the network connection you're sharing, and not located in an OFAC-sanctioned jurisdiction. You can run the daemon on hardware you own or have explicit permission to use.",
  },
  {
    h: "P2. What you're providing",
    body: "Idle CPU/GPU cycles + bandwidth measured in bytes, optionally with a daily cap. Default cap: 50 GB / month / device. You can change it from /provide/schedule at any time. We never bill you; you bill us.",
  },
  {
    h: "P3. What you're paid",
    body: "Choose: USD via Stripe Connect, $GRID on Solana, free unlimited VPN credit, or donation to a charity from the audited list. Payout floors apply per currency (Stripe: $10/month; $GRID: 100 $GRID = ~$10; charity: $5). Below floor → carried to next month.",
  },
  {
    h: "P4. Abuse",
    body: "Every byte you relay is logged in your transparency feed (/provide/audit). You can block any destination / category / customer / country at any time. We default-deny NCMEC + PhishTank + Google Safe Browsing categories on your behalf. You are NOT liable for customer-initiated traffic that you blocked or that bypassed our default-deny.",
  },
  {
    h: "P5. Termination",
    body: "Either party can terminate at any time. Stop the daemon and you stop earning; your historical earnings remain payable. We can suspend a provider if abuse logs show your machine was the source of a confirmed policy violation (rare; <0.01% of pairs over history).",
  },
];

const CUSTOMER_TOS = [
  {
    h: "C1. Acceptable use",
    body: "Buy bandwidth, compute, GPU, iOS-build time. NOT for: NCMEC-flagged content, financial fraud, credential stuffing against accounts you don't own, copyright infringement at scale, DDoS, malware C2, weapons-of-mass-destruction research, election interference, CSAM. Per-byte audit applies — we WILL serve subpoenas attesting to your specific traffic.",
  },
  {
    h: "C2. Billing",
    body: "Per-byte / per-second / per-minute meter. Stripe (USD/EUR/GBP/JPY/AUD/CAD/SGD) or $GRID. First $5 free. Net-30 invoicing available for enterprises ≥$5k/month commit. Tax handled per jurisdiction; you supply VAT/GST/sales-tax IDs on /account/billing.",
  },
  {
    h: "C3. SLA",
    body: "Bandwidth: 99.5% monthly availability for residential SOCKS5 endpoints. Compute: 99.9% job-accept rate. iOS-build: 99% workflow-finish rate within stated wall-time budget. Credit refunded for each minute we missed the SLA, automatically.",
  },
  {
    h: "C4. Data",
    body: "We do NOT cache, sample, or copy your traffic except as needed for the policy-block check (URL hashed against the abuse blocklist before relay; URL itself discarded on miss). Audit logs record only: destination hostname (no path/query), bytes shipped, providers used, decision outcome.",
  },
  {
    h: "C5. Termination",
    body: "Cancel any time from /customer/billing. Outstanding workloads complete or refund. Account closure: 30-day retention for tax purposes (per /privacy §6), then full deletion including audit + payment-processor records.",
  },
];

const AUP = [
  {
    h: "A1. The hard list",
    body: "NCMEC PhotoDNA hits, Google Safe Browsing malware/phishing, PhishTank, IWF, sanctioned-country destinations (per OFAC + EU + UK sanctions lists, refreshed daily). These are default-deny at the proxy-gateway layer — not even an opt-in option.",
  },
  {
    h: "A2. The discretionary list",
    body: "Scraping public sites in violation of their robots.txt at >10 req/sec sustained, harvesting personally-identifiable information from authenticated dashboards, credential-stuffing test workloads against production endpoints you don't own. Default-deny, opt-out only via signed proof of authorization (e.g., bug-bounty program ID).",
  },
  {
    h: "A3. Enforcement",
    body: "Tier 1 (first warning): the workload is paused, you get an email, you have 72h to respond or auto-unpause if no enforcement match. Tier 2 (repeated): API key revoked, organization billing freeze, written response required. Tier 3 (confirmed abuse): account terminated, on-chain audit record published.",
  },
];

export default function TermsPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Legal"
        title="Terms of Service + Acceptable Use Policy"
        body={`Provider TOS, Customer TOS, and AUP. Plain English where possible. Last updated ${LAST_UPDATED}.`}
      />
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Provider Terms of Service
          </h2>
          <dl className="mt-8 space-y-8">
            {PROVIDER_TOS.map((row) => (
              <div key={row.h}>
                <dt className="text-base font-semibold text-foreground">
                  {row.h}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {row.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Customer Terms of Service
          </h2>
          <dl className="mt-8 space-y-8">
            {CUSTOMER_TOS.map((row) => (
              <div key={row.h}>
                <dt className="text-base font-semibold text-foreground">
                  {row.h}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {row.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Acceptable Use Policy
          </h2>
          <dl className="mt-8 space-y-8">
            {AUP.map((row) => (
              <div key={row.h}>
                <dt className="text-base font-semibold text-foreground">
                  {row.h}
                </dt>
                <dd className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {row.body}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </MarketingShell>
  );
}
