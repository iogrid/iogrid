import Link from "next/link";
import { ArrowRight, Cpu, ShieldCheck, Boxes } from "lucide-react";
import { ThemeToggle } from "@/components/theme-toggle";

/**
 * Landing page — Phase 2.1 of EPIC #422.
 *
 * Aesthetic: Linear / Notion / Vercel. NO decorative illustrations,
 * NO purple-pink gradients, NO "techy" cliches. The proposition
 * carries the page; whitespace and typography do the heavy lifting.
 *
 * Structure:
 *   1. Slim top nav — wordmark + primary links + theme toggle.
 *   2. Hero — one-sentence proposition + 1 primary CTA + 1 secondary CTA.
 *   3. Three product pillars (Provide, Customer, VPN) — icon-only.
 *   4. Footer — single-row, minimal.
 *
 * Explicitly omitted (per EPIC #422 founder direction):
 *   - "Techy geek guys" illustrations / isometric scenes.
 *   - Fabricated trust metrics. The trust section will be added in a
 *     follow-up PR once real provider/country/byte counters land via
 *     telemetry-svc — fabricating "X providers in Y countries" today
 *     would be theater (see PRINCIPLES.md / NEVER SPECULATE).
 *
 * The component is a pure Server Component — no client interactivity
 * needed for the static landing surface; the only island is the
 * <ThemeToggle/>, which is already a 'use client' boundary.
 */
export default function HomePage() {
  return (
    <main className="min-h-screen bg-background text-foreground">
      <SiteHeader />
      <Hero />
      <Pillars />
      <SiteFooter />
    </main>
  );
}

/* ----------------------------- Header ---------------------------- */

function SiteHeader() {
  return (
    <header className="border-b border-border">
      <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-6">
        <Link
          href="/"
          className="text-sm font-semibold tracking-tight"
          aria-label="iogrid home"
        >
          iogrid
        </Link>
        <nav aria-label="Primary" className="hidden items-center gap-6 md:flex">
          <HeaderLink href="/provide">Provide</HeaderLink>
          <HeaderLink href="/customer">Customer</HeaderLink>
          <HeaderLink href="/vpn">VPN</HeaderLink>
          <HeaderLink href="/account">Account</HeaderLink>
        </nav>
        <div className="flex items-center gap-3">
          <ThemeToggle />
          <Link
            href="/install"
            className="hidden rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 sm:inline-flex"
          >
            Get iogrid
          </Link>
        </div>
      </div>
    </header>
  );
}

function HeaderLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <Link
      href={href}
      className="text-sm text-muted-foreground transition-colors hover:text-foreground"
    >
      {children}
    </Link>
  );
}

/* ------------------------------ Hero ----------------------------- */

function Hero() {
  return (
    <section className="border-b border-border">
      <div className="mx-auto max-w-6xl px-6 py-24 md:py-32">
        {/* Tagline pill — single accent surface, sets the premium tone
            without resorting to decorative graphics. */}
        <div className="mb-8 inline-flex items-center gap-2 rounded-full border border-border px-3 py-1 text-xs text-muted-foreground">
          <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-primary" />
          Distributed compute, owned by the people who run it.
        </div>

        <h1 className="max-w-3xl text-4xl font-semibold tracking-tight text-foreground md:text-5xl lg:text-6xl">
          Rent your idle machine.{" "}
          <span className="text-muted-foreground">
            Or rent the whole network.
          </span>
        </h1>

        <p className="mt-6 max-w-2xl text-lg leading-relaxed text-muted-foreground">
          iogrid pools idle CPUs, GPUs, and home internet connections into a
          single schedulable mesh. Providers earn for the spare cycles they
          share. Customers run residential proxy, container, and macOS-iOS
          build workloads on it.
        </p>

        <div className="mt-10 flex flex-wrap items-center gap-3">
          <Link
            href="/install"
            className="inline-flex items-center gap-2 rounded-md bg-primary px-5 py-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Install the daemon
            <ArrowRight aria-hidden className="h-4 w-4" />
          </Link>
          <Link
            href="/customer"
            className="inline-flex items-center gap-2 rounded-md border border-border bg-background px-5 py-3 text-sm font-medium text-foreground transition-colors hover:border-foreground/40 hover:bg-muted"
          >
            For customers
          </Link>
        </div>

        <p className="mt-6 text-xs text-muted-foreground">
          Single static binary. macOS, Linux, Windows. No background account
          required to install.
        </p>
      </div>
    </section>
  );
}

/* --------------------------- Pillars ----------------------------- */

interface Pillar {
  href: string;
  icon: React.ElementType;
  label: string;
  title: string;
  blurb: string;
}

const PILLARS: Pillar[] = [
  {
    href: "/provide",
    icon: Cpu,
    label: "Provide",
    title: "Earn from idle hardware.",
    blurb:
      "Donate spare CPU, GPU, and bandwidth. Pick cash, free VPN, or a charity payout. Per-byte transparency, opt-in for every workload class.",
  },
  {
    href: "/customer",
    icon: Boxes,
    label: "Customer",
    title: "Run workloads at edge prices.",
    blurb:
      "Residential-IP proxy, container compute, GPU inference, native macOS iOS-build CI. SDKs in TypeScript, Python, Go, Java. Pay by usage.",
  },
  {
    href: "/vpn",
    icon: ShieldCheck,
    label: "VPN",
    title: "Free, included with the daemon.",
    blurb:
      "Every provider gets unlimited VPN as part of the deal. Consume-only on mobile (iOS / Android). No upsell tiers, no logs sold.",
  },
];

function Pillars() {
  return (
    <section
      aria-labelledby="pillars-heading"
      className="border-b border-border"
    >
      <div className="mx-auto max-w-6xl px-6 py-20">
        <h2
          id="pillars-heading"
          className="text-xs font-medium uppercase tracking-wider text-muted-foreground"
        >
          Three sides, one mesh
        </h2>
        <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3">
          {PILLARS.map((p) => (
            <PillarCard key={p.href} pillar={p} />
          ))}
        </div>
      </div>
    </section>
  );
}

function PillarCard({ pillar }: { pillar: Pillar }) {
  const { href, icon: Icon, label, title, blurb } = pillar;
  return (
    <Link
      href={href}
      className="group flex flex-col gap-4 bg-background p-8 transition-colors hover:bg-muted"
    >
      <div className="flex items-center gap-3">
        <span className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-background text-foreground">
          <Icon aria-hidden className="h-4 w-4" />
        </span>
        <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </span>
      </div>
      <h3 className="text-lg font-semibold tracking-tight text-foreground">
        {title}
      </h3>
      <p className="text-sm leading-relaxed text-muted-foreground">{blurb}</p>
      <span className="mt-auto inline-flex items-center gap-1 text-sm font-medium text-foreground">
        Learn more
        <ArrowRight
          aria-hidden
          className="h-4 w-4 transition-transform group-hover:translate-x-0.5"
        />
      </span>
    </Link>
  );
}

/* ----------------------------- Footer ---------------------------- */

function SiteFooter() {
  return (
    <footer>
      <div className="mx-auto flex max-w-6xl flex-col items-start gap-4 px-6 py-10 text-xs text-muted-foreground md:flex-row md:items-center md:justify-between">
        <div className="flex items-center gap-2">
          <span className="font-semibold text-foreground">iogrid</span>
          <span aria-hidden>·</span>
          <span>Open-source. Operator-owned.</span>
        </div>
        <nav aria-label="Footer">
          <ul className="flex flex-wrap gap-x-6 gap-y-2">
            <li>
              <FooterLink href="/install">Install</FooterLink>
            </li>
            <li>
              <FooterLink href="/customer">Customer docs</FooterLink>
            </li>
            <li>
              <FooterLink href="/provide">Provider terms</FooterLink>
            </li>
            <li>
              <FooterLink href="https://github.com/iogrid/iogrid">
                GitHub
              </FooterLink>
            </li>
          </ul>
        </nav>
      </div>
    </footer>
  );
}

function FooterLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <Link href={href} className="transition-colors hover:text-foreground">
      {children}
    </Link>
  );
}
