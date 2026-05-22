import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "iogrid VPN — Free 2 GB / month, Plus $2.99, Pro $4.99",
  description:
    "Mesh VPN funded by enterprise customers, not your data. Free 2 GB / month, $2.99 Plus, $4.99 Pro. No logs sold. Same daemon ships on iOS, Android, Mac, Windows, Linux.",
};

const STEPS = [
  {
    n: "1",
    title: "Install the daemon (or the mobile app)",
    body: "On a PC or Mac, install iogridd from /install — it runs as a tiny background service and routes your apps through the mesh. On iOS or Android, install the iogrid mobile app from the App Store or Play Store. Same identity, all platforms.",
  },
  {
    n: "2",
    title: "Pick an exit region",
    body: "Choose any region in the mesh. The router connects you over WireGuard to an opted-in provider in that geography. Sessions are sticky to the same exit while you stay on the same network.",
  },
  {
    n: "3",
    title: "Browse — your bytes do not get sold",
    body: "Free tier traffic comes from the same residential providers that power the proxy network, but the routing log is keyed to you only as a counter, not a sellable profile. We make money on the B2B side, not by reselling your DNS.",
  },
];

const PRICING = [
  {
    tier: "Free",
    price: "$0 / month",
    bullets: [
      "2 GB / month",
      "All public exit regions",
      "WireGuard + iOS Network Extension",
      "No logs sold — ever",
    ],
  },
  {
    tier: "Plus",
    price: "$2.99 / month",
    bullets: [
      "Unlimited bandwidth",
      "All exit regions",
      "Up to 5 devices",
      "Standard support",
    ],
    highlight: true,
  },
  {
    tier: "Pro",
    price: "$4.99 / month",
    bullets: [
      "Unlimited bandwidth",
      "Per-app exit selection",
      "Up to 10 devices",
      "DNS-over-HTTPS + tracker blocking",
      "Priority support",
    ],
  },
];

const USE_CASES = [
  {
    title: "Travel + public Wi-Fi",
    body: "A free unlimited-ish layer over your hotel, airport, or café Wi-Fi. Exit regions cover the US, EU, UK, and major APAC markets. WireGuard reconnects fast when you switch networks.",
  },
  {
    title: "Region unlock for streaming",
    body: "Pick an exit region in the country where the content is licensed. Latency is consumer-residential, not datacenter, so most services do not flag the IP the way they do with traditional VPN ranges.",
  },
  {
    title: "Daily privacy hygiene",
    body: "Keep your ISP and the next coffee-shop guest out of your DNS without paying $5–13 / month to NordVPN or ExpressVPN. Pro tier adds DNS-over-HTTPS and a tracker block list at the gateway.",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "How is this free? What is the catch?",
    a: "iogrid VPN runs on the same residential mesh that enterprise customers pay $0.30–0.60 / GB to use for proxy traffic. The B2B revenue subsidises the consumer VPN. You are not the product — there is no logging or reselling of your traffic. The free tier exists to onboard providers and consumers into the same network; the bandwidth cap covers our marginal cost.",
  },
  {
    q: "What is the difference vs Hola VPN?",
    a: "Hola also runs on residential providers, but providers never see what is routed through them and they cannot block specific customers or categories. iogrid providers see a live audit log of every category running through their connection and can revoke access per-category, per-customer, per-destination with one click. We are the ethical Hola: same architecture, consensual.",
  },
  {
    q: "Do you keep logs?",
    a: "We keep a per-request routing record (timestamp, exit region, byte counts, category) for billing and abuse defense. We do not keep DNS query logs, URL paths, or request bodies. We do not sell, share, or rent the routing log to third parties. The audit log is the SAME data customers see — it is the transparency dashboard, not a hidden surveillance layer.",
  },
  {
    q: "Which platforms?",
    a: "iOS (App Store, Network Extension), Android (Play Store), macOS, Windows, Linux. The PC/Mac daemon is the same binary the provider network uses — flip a flag and the same install becomes a consumer-only client. Mobile clients are consume-only by App Store policy.",
  },
  {
    q: "Free tier cap — what happens at 2 GB?",
    a: "When you hit 2 GB in a calendar month, the tunnel drops to a slow lane (~256 kbps) instead of cutting off. Most users on this lane upgrade to Plus; some keep using it for email and chat. The counter resets on the first of the month, in your local time.",
  },
  {
    q: "Is there a refund policy?",
    a: "Yes — 7-day no-questions-asked refund on Plus and Pro from the date you subscribe. Stripe handles the refund; we do not gatekeep with cancellation flows.",
  },
];

export default function VpnPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="VPN"
        title="Mesh VPN funded by enterprise customers, not your data."
        subtitle="Free 2 GB / month. Plus $2.99 unlimited. Pro $4.99 with per-app exits and DNS-over-HTTPS. Same daemon, all platforms. The audit log is public — providers see exactly what they relay, and so do you."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/install"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Install the app
            </Link>
            <Link
              href="/vpn/upgrade"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Upgrade to Plus or Pro
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
            iogrid VPN is a consumer-facing layer on the same residential mesh
            that powers our proxy product. Your traffic exits through an
            opted-in home connection in the region you pick. WireGuard handles
            the tunnel; the iogrid coordinator handles routing, billing, and
            the transparency log that providers and customers both see.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            Unlike datacenter VPNs (NordVPN, ProtonVPN), the exit is a real
            residential IP — services that flag VPN ranges do not flag this.
            Unlike Hola, every provider on the network opted in, sees a
            category-level audit of what they relay, and can revoke at any
            time. There is no hidden mining of your DNS to keep the lights on.
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
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Pricing
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {PRICING.map((p) => (
              <div
                key={p.tier}
                className={
                  p.highlight
                    ? "flex flex-col gap-3 bg-muted p-8"
                    : "flex flex-col gap-3 bg-background p-8"
                }
              >
                <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  {p.tier}
                </span>
                <p className="text-2xl font-semibold text-foreground">
                  {p.price}
                </p>
                <ul className="mt-2 space-y-2 text-sm leading-relaxed text-muted-foreground">
                  {p.bullets.map((b) => (
                    <li key={b} className="flex items-start gap-2">
                      <span aria-hidden className="mt-0.5 text-foreground">
                        •
                      </span>
                      <span>{b}</span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Reference: NordVPN $3–13 / month, ProtonVPN free (slow) + $4–10
            paid, Mullvad $5.50 / month. iogrid is deliberately under the
            market because B2B revenue carries the marginal cost — the consumer
            tier is acquisition, not the profit center.
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

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Get started
          </h2>
          <p className="mt-6 text-base leading-relaxed text-muted-foreground">
            Install the daemon on your PC or Mac, or the iogrid app on your
            phone. The free tier needs no signup beyond an email; Plus and Pro
            unlock through Stripe Checkout.
          </p>
          <div className="mt-6 flex flex-wrap gap-3">
            <Link
              href="/install"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Install the app
            </Link>
            <Link
              href="/vpn/upgrade"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Upgrade to Plus or Pro
            </Link>
            <Link
              href="/transparency"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              See the audit log
            </Link>
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
