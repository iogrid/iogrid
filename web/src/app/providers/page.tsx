import type { Metadata } from "next";
import Link from "next/link";
import { MarketingShell } from "@/components/marketing/marketing-shell";
import { PageHero } from "@/components/marketing/page-hero";

export const metadata: Metadata = {
  title: "For providers — cash, free VPN, $GRID, or charity",
  description:
    "One install on a Mac or PC. Pick your payout currency. Per-byte transparency. Block any category, customer, or destination at any time.",
};

const STEPS = [
  {
    n: "1",
    title: "Install the daemon",
    body: "Single static binary for macOS, Linux, and Windows on both Intel and Apple Silicon / ARM. ~12 MB. Defaults to idle-only (5-minute threshold) and a 50 GB / month bandwidth cap.",
  },
  {
    n: "2",
    title: "Pick your payout currency",
    body: "Cash via Stripe Connect ($10 minimum), free unlimited iogrid VPN on all your devices, $GRID tokens with optional 1.25× / 1.5× / 2× lockup multiplier, or charity match. Switch any time.",
  },
  {
    n: "3",
    title: "Watch every byte",
    body: "The dashboard shows live category labels for every byte that has transited your IP, broken down by customer and destination. Block any of those three with one click. No support ticket required.",
  },
];

const PAYOUTS = [
  {
    tier: "Cash",
    detail: "$0.30 per GB of bandwidth shared. Stripe Connect payout with a $10 minimum threshold. 1099 issued at year-end if you exceed $600 (US providers).",
  },
  {
    tier: "Free VPN",
    detail: "Your bandwidth-share pays for unlimited iogrid VPN on all your devices — iOS, Android, Mac, Windows, Linux. Equivalent to NordVPN's $5/month plan. Free forever as long as you keep providing.",
  },
  {
    tier: "Charity",
    detail: "Your earnings are donated to a cause you pick — EFF, Tor Project, Wikipedia, Doctors Without Borders, or any 501(c)(3) we have onboarded. We forward 100% of the donation.",
  },
  {
    tier: "$GRID",
    detail: "Native work-token on Solana. Lockup tier (30-day default, up to 1-year cliff for 2× multiplier) sets your earnings multiplier. See the token page for the full mechanics.",
  },
];

const HARDWARE = [
  {
    title: "Home Linux / Windows PC",
    body: "Sharing 30 GB / month of bandwidth: roughly $9 / month in cash, or unlimited VPN, or proportional $GRID. Workloads: bandwidth proxy, Docker compute, GPU inference if you have a 4090 / 5090.",
  },
  {
    title: "Apple Silicon Mac",
    body: "Sharing 30 GB + 4 idle Xcode hours per day: roughly $154 / month combined (bandwidth + iOS-build CI), or proportional VPN + $GRID. 15× the economics of a bandwidth-only network.",
  },
  {
    title: "Homelab / always-on box",
    body: "Always-on Linux box with spare CPU + bandwidth budget runs Docker workloads, GPU inference, and the bandwidth proxy continuously. Calendar windows and cap controls let you keep your other apps responsive.",
  },
];

const CONTROLS = [
  {
    title: "Per-category opt-in",
    body: "Choose which categories your hardware can serve — e-commerce monitoring, SEO scraping, ad verification, AI training data, GPU inference, iOS builds. Toggle any of them off without uninstalling.",
  },
  {
    title: "Per-customer block",
    body: "Don't want a specific company's traffic on your IP? Block them. The audit log shows you every customer your bandwidth has served; one click blocks them forever from your provider.",
  },
  {
    title: "Per-destination block",
    body: "Block specific domains entirely — your favourite news site, your competitor, anything you want kept off your IP. The block lives on your daemon, not at the coordinator, so it cannot be silently reverted.",
  },
];

const FAQ: { q: string; a: React.ReactNode }[] = [
  {
    q: "What hardware can be a provider?",
    a: "Desktops, laptops, and homelab boxes on macOS, Linux, or Windows (Intel / Apple Silicon / ARM64). Mobile devices (iOS / Android) are consume-only — they can use the VPN but they cannot earn. This is a platform policy designed around battery, thermals, and store-policy compliance.",
  },
  {
    q: "Will this slow my computer down?",
    a: "By default the daemon runs in idle-only mode (5-minute threshold), with a 30% CPU cap and a 25% memory cap. Workloads run under cgroup limits so they cannot starve your other apps. If you do notice an impact, the dashboard lets you tighten the caps or set a calendar window (e.g. nights and weekends only).",
  },
  {
    q: "What about my electricity bill?",
    a: "Bandwidth-only providers see no measurable change. CPU and GPU workloads draw real power — the dashboard estimates draw against your declared local kWh price so you can see if a workload is net-positive for you. Most providers cap CPU / GPU workloads to night-time hours where draw is cheapest.",
  },
  {
    q: "Can a customer see who I am?",
    a: "No. Customers see workload IDs, regions, and bandwidth bytes. Providers are pseudonymous from a customer's perspective. The audit log identifies the customer to the provider, not the other way around.",
  },
  {
    q: "What happens if my IP is used for something illegal?",
    a: "Anti-abuse runs at the coordinator before bytes reach your daemon — CSAM hashes, fraud blocklists, sanctions lists, customer category disallowances. If something slips through, the audit log gives you proof of what was attempted and from which customer. We handle takedown and law-enforcement liaison; we never disclose your identity without a court order.",
  },
];

export default function ProvidersPage() {
  return (
    <MarketingShell>
      <PageHero
        eyebrow="Providers"
        title="Earn from idle hardware, pick how you get paid."
        subtitle="One install on a Mac or PC. Cash via Stripe, free unlimited VPN, $GRID tokens, or charity payouts. Per-byte transparency. Block any category, customer, or destination — without uninstalling."
      />

      <section className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-16 md:py-20">
          <div className="flex flex-wrap gap-3">
            <Link
              href="/install"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Install the daemon
            </Link>
            <Link
              href="/provider"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Open the dashboard
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
            iogrid pays you for idle bandwidth, CPU, GPU, or Xcode hours on
            hardware you already own. The daemon is a single static binary,
            opt-in per category, and audited per byte. You see exactly what
            your IP is doing and you can stop any of it instantly.
          </p>
          <p className="mt-4 text-base leading-relaxed text-muted-foreground">
            Power asymmetry favours the supplier. Providers can kick customers
            off their hardware; customers cannot kick providers out of the
            network.
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
        <div className="mx-auto max-w-4xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Payout tiers
          </h2>
          <div className="mt-6 overflow-hidden rounded-lg border border-border">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted">
                <tr>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    Tier
                  </th>
                  <th className="px-4 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                    What you get
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border bg-background">
                {PAYOUTS.map((row) => (
                  <tr key={row.tier}>
                    <td className="px-4 py-3 text-foreground">{row.tier}</td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {row.detail}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Customer pricing: bandwidth $0.30–$0.60 / GB, Docker $0.018 /
            vCPU-hour, GPU $0.20–$2.00 / GPU-hour, iOS builds $0.04 /
            Xcode-minute. See the{" "}
            <Link
              href="/pricing"
              className="text-foreground underline-offset-2 hover:underline"
            >
              pricing page
            </Link>{" "}
            for the full breakdown.
          </p>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What your hardware earns
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {HARDWARE.map((h) => (
              <div key={h.title} className="flex flex-col gap-3 bg-background p-8">
                <h3 className="text-base font-semibold text-foreground">
                  {h.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {h.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="border-b border-border">
        <div className="mx-auto max-w-5xl px-6 py-16">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            What you control
          </h2>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
            {CONTROLS.map((c) => (
              <div key={c.title} className="flex flex-col gap-3 bg-background p-8">
                <h3 className="text-base font-semibold text-foreground">
                  {c.title}
                </h3>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {c.body}
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
            Download the daemon and pair it with your account in under five
            minutes.
          </p>
          <div className="mt-6 flex flex-wrap gap-3">
            <Link
              href="/install"
              className="inline-flex items-center justify-center rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background transition-colors hover:bg-foreground/90"
            >
              Install the daemon
            </Link>
            <Link
              href="/about"
              className="inline-flex items-center justify-center rounded-md border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
            >
              Read our principles
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
