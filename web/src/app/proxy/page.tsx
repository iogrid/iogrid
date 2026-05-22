import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "Residential proxy — $0.30–$0.60 / GB with per-byte audit",
  description:
    "SOCKS5 + HTTP CONNECT proxy on opted-in residential IPs. Geo-targeted, session-sticky, with cryptographic audit. 10–30× cheaper than Bright Data.",
};

const STEPS = [
  {
    n: "1",
    title: "Connect over SOCKS5 / HTTPS CONNECT",
    body: "Point any HTTP client that speaks SOCKS5h or HTTP CONNECT at proxy.iogrid.org:443. Authenticate with your API key. Pass region and session preferences as connect headers.",
  },
  {
    n: "2",
    title: "Router picks a provider",
    body: "The workload-router picks an opted-in provider matching geo, category eligibility, recent uptime, and reputation. The session is sticky to the same provider for up to 30 minutes.",
  },
  {
    n: "3",
    title: "Request egresses from a real residential IP",
    body: "Your request traverses a WireGuard tunnel into the provider's home network and out their ISP. Bytes in and out are metered at the coordinator for both billing and the per-byte audit log.",
  },
];

const PRICING = [
  { col: "Per GB", value: "$0.30 – $0.60" },
  { col: "Geo-targeting (country)", value: "Included" },
  { col: "Geo-targeting (city)", value: "Included" },
  { col: "Session stickiness", value: "Up to 30 minutes" },
  { col: "Audit log retention", value: "90 days, downloadable" },
  { col: "$GRID discount", value: "20% off list price" },
];

const USE_CASES = [
  {
    title: "E-commerce price monitoring",
    body: "Pull product pages from a residential IP that does not trigger the rate limits and CAPTCHA walls reserved for datacenter ranges. Geo-target the country for region-specific pricing.",
  },
  {
    title: "SEO / SERP scraping",
    body: "Query search engines from real consumer IPs across regions. Session stickiness lets you carry cookies through a multi-page result without rotating partway.",
  },
  {
    title: "Ad verification + brand safety",
    body: "Render ad placements as they appear to actual users in a target country. The audit log gives you a cryptographic record of which IP saw which creative, useful for compliance reporting.",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "What categories are allowed?",
    a: "E-commerce, SEO, ad verification, AI training data, threat intelligence, brand protection, and travel aggregation by default. Lead-generation scraping on LinkedIn-style surfaces, social-media intelligence, and political influence operations are blocked at the gateway. Providers can opt in to additional categories.",
  },
  {
    q: "What does the per-byte audit log show?",
    a: "Per-request: timestamp, customer ID, destination domain (not URL), category, byte counts, and the eligible-categories signature. The coordinator cannot decrypt customer HTTPS payloads — the log is a routing record, not a content log.",
  },
  {
    q: "How is this different from Bright Data or Honeygain?",
    a: "Same architecture, but providers see and can revoke their participation per category, per customer, and per destination. Pricing is 10–30× under Bright Data ($5–20/GB retail). Providers can also choose free VPN or charity payouts instead of cash.",
  },
  {
    q: "Will my IPs get banned?",
    a: "Residential IPs are not pre-flagged the way datacenter ranges are. That said, abusive traffic patterns can earn a per-destination ban — the audit log helps you spot it before it happens, and the scheduler avoids over-using a single provider.",
  },
  {
    q: "Is the protocol authenticated end-to-end?",
    a: "Yes. Customer-to-coordinator is TLS. Coordinator-to-provider is WireGuard. Provider-to-destination is whatever the destination negotiates (typically HTTPS). No party other than you and the destination sees the request body.",
  },
];

const CURL_SNIPPET = `# SOCKS5 with a session pinned to a US-East provider
curl -x socks5h://USER:PASS@proxy.iogrid.org:443 \\
  -H "X-Iogrid-Region: us-east-1" \\
  -H "X-Iogrid-Session: campaign-q4-watch-42" \\
  -H "X-Iogrid-Category: e_commerce" \\
  https://www.example.com/product/42`;

const SDK_SNIPPET = `import { IogridClient } from '@iogrid/sdk';

const iogrid = new IogridClient({ apiKey: process.env.IOGRID_API_KEY! });

const w = await iogrid.createWorkload({
  type: 'BANDWIDTH',
  priority: 'HIGH',
  bandwidth: {
    targetUrl: 'https://www.example.com/product/42',
    method: 'GET',
    preferredRegion: 'us-east-1',
    category: 'e_commerce',
    sessionId: 'campaign-q4-watch-42',
  },
  labels: { campaign: 'price-watch-q4' },
});`;

export default function ProxyPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Proxy"
        title="Residential IP proxy with full transparency."
        subtitle="SOCKS5-over-TLS and HTTP CONNECT to providers who consented to bandwidth-share. Per-byte audit log, per-customer billing, per-provider opt-out, 10–30× cheaper than incumbents."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Get a proxy key
            </Link>
            <Link
              href="/pricing"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              See pricing
            </Link>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What it is
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            iogrid Proxy is a residential-IP proxy network whose providers run
            the iogrid daemon on their home PCs and Macs and have consented to
            share bandwidth on a per-category basis. Your traffic exits from a
            real ISP-issued IP in the geography you ask for.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            Unlike legacy networks, every byte is logged at the coordinator
            against a category and customer ID. Providers can audit what is
            running through their connection and block by category, by
            customer, or by destination at any time without uninstalling
            anything.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            How it works
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {STEPS.map((s) => (
              <div key={s.n} className="flex flex-col gap-3 bg-background p-8">
                <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  Step {s.n}
                </span>
                <h3 className="text-base font-semibold text-foreground">
                  {s.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {s.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Pricing
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <tbody className="divide-y divide-border bg-background">
                {PRICING.map((row) => (
                  <tr key={row.col}>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.col}
                    </td>
                    <td className="px-4 py-3 text-foreground">{row.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Reference: Bright Data retail $5–20/GB, Honeygain/Pawns $5–15/GB.
            iogrid is deliberately 10–30× cheaper because provider payouts can
            be in free VPN or $GRID, not just cash.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What you can do with it
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {USE_CASES.map((u) => (
              <div key={u.title} className="flex flex-col gap-3 bg-background p-8">
                <h3 className="text-base font-semibold text-foreground">
                  {u.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {u.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Any HTTP client that speaks SOCKS5h works without changes. The SDK
            adds session management, retries, and audit-log lookup.
          </p>
          <pre className="mt-6 overflow-x-auto rounded-lg border border-border bg-muted p-6 text-xs leading-relaxed text-foreground">
            <code>{CURL_SNIPPET}</code>
          </pre>
          <pre className="mt-4 overflow-x-auto rounded-lg border border-border bg-muted p-6 text-xs leading-relaxed text-foreground">
            <code>{SDK_SNIPPET}</code>
          </pre>
          <div className="mt-6 flex flex-wrap gap-3">
            <Link
              href="/customer"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Get an API key
            </Link>
            <Link
              href="/docs"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read the SDK docs
            </Link>
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            FAQ
          </h2>
          <dl className="mt-8 space-y-8">
            {FAQ.map((row) => (
              <div key={row.q}>
                <dt className="text-base font-semibold text-foreground">
                  {row.q}
                </dt>
                <dd className="mt-2 text-base leading-relaxed text-muted-foreground">
                  {row.a}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </section>
    </MarketingShell>
  );
}
