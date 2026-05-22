import Link from "next/link";
import type { Metadata } from "next";
import { Cpu, Boxes, ShieldCheck } from "lucide-react";

export const metadata: Metadata = {
  title: "Welcome to iogrid",
  description: "Pick where to start. You can switch anytime.",
};

/**
 * First-sign-in landing — explicit persona picker so users land in
 * the right virtual app instead of being shown all three personas at
 * once. Founder direction 2026-05-22: 99% of users are exactly ONE
 * role; surfacing the other two at all times is noise.
 *
 * Choice gets persisted server-side at identity-svc.users.preferred_landing_role
 * + as a JWT claim. Subsequent sign-ins go straight to that virtual
 * app; the rail is always available to switch.
 *
 * Existing users get to see this once on their first sign-in after
 * deploy (preferred_landing_role NULL → picker; NOT NULL → straight
 * to that app).
 */

interface PersonaOption {
  href: string;
  icon: React.ElementType;
  badge: string;
  title: string;
  blurb: string;
  cta: string;
}

const OPTIONS: PersonaOption[] = [
  {
    href: "/provider",
    icon: Cpu,
    badge: "Provider",
    title: "Share my hardware and earn.",
    blurb:
      "Donate spare CPU, GPU, and bandwidth. Pick cash, free VPN, or charity payouts. Per-byte audit, opt-out anytime.",
    cta: "Become a provider",
  },
  {
    href: "/customer",
    icon: Boxes,
    badge: "Customer",
    title: "Build with iogrid services.",
    blurb:
      "Bandwidth proxy, Docker, GPU inference, native macOS iOS-build CI. SDKs in TS / Python / Go / Java. Pay by usage.",
    cta: "Start building",
  },
  {
    href: "/vpn",
    icon: ShieldCheck,
    badge: "VPN",
    title: "Use the free VPN.",
    blurb:
      "Free unlimited bandwidth via the daemon. Or paid tiers from $2.99 with more locations and tracker blocking.",
    cta: "Activate VPN",
  },
];

export default function WelcomePage() {
  return (
    <main className="min-h-screen bg-background text-foreground">
      <header className="border-b border-border">
        <div className="container-page flex h-16 items-center">
          <Link
            href="/"
            aria-label="iogrid home"
            className="flex h-9 w-9 items-center justify-center rounded-md bg-primary-500 text-sm font-bold text-white"
          >
            ig
          </Link>
          <span className="ml-3 text-sm text-muted-foreground">
            Welcome to iogrid
          </span>
        </div>
      </header>

      <section className="container-page py-20">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="h-section text-foreground">
            Pick where to start.
          </h1>
          <p className="mt-4 text-lead">
            You can switch anytime from the left rail. Everyone gets all three
            roles by default — this is just to land you in the right place.
          </p>
        </div>

        <div className="mx-auto mt-12 grid max-w-6xl gap-6 md:grid-cols-3">
          {OPTIONS.map((opt) => {
            const Icon = opt.icon;
            return (
              <Link
                key={opt.href}
                href={`${opt.href}?from=welcome`}
                className="card group flex flex-col gap-4 transition hover:border-primary-500"
              >
                <div className="flex items-center gap-3">
                  <span className="inline-flex h-10 w-10 items-center justify-center rounded-md bg-primary-50 text-primary-700 dark:bg-primary-900 dark:text-primary-200">
                    <Icon className="h-5 w-5" aria-hidden />
                  </span>
                  <span className="pill">{opt.badge}</span>
                </div>
                <h2 className="h-card text-foreground">{opt.title}</h2>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  {opt.blurb}
                </p>
                <span className="btn-primary mt-auto self-start">
                  {opt.cta} →
                </span>
              </Link>
            );
          })}
        </div>
      </section>
    </main>
  );
}
