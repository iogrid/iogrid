import Link from "next/link";
import * as React from "react";
import { cn } from "@/lib/utils";
import { ThemeToggle } from "@/components/theme-toggle";

/**
 * PortalShell is the chrome shared by every authenticated user-facing
 * surface (/provide, /customer, /vpn, /account). It enforces a single
 * layout pattern so sub-agents and follow-up PRs cannot reintroduce
 * bespoke top-bars or sidebars.
 *
 * Aesthetic (EPIC #422 Phase 2.2): Linear / Notion / Vercel — single
 * border-driven elevation, no shadows, no rainbow chrome, no
 * illustrations. Every color resolves through the semantic tokens
 * defined in design-tokens.css so the dark theme flips for free.
 *
 * Server-component-safe (async). Interactive bits (user menu, sign-out)
 * live in dedicated client islands.
 *
 * Strict-separation invariant (EPIC #422 Phase 1): this shell renders
 * the four user-facing tabs only — Provide / Customer / VPN / Account.
 * It NEVER renders an "Admin" tab; the admin console is an entirely
 * separate Next.js app on admin.iogrid.org with its own AdminShell,
 * its own cookie, and its own session. An admin who is also a provider
 * uses two hosts (one cookie each) to do their two jobs.
 */

export interface NavItem {
  href: string;
  label: string;
  description?: string;
}

export interface PortalShellProps {
  title: string;
  subtitle?: string;
  nav: NavItem[];
  activeHref?: string;
  /** Section accent rendered to the left of the title. */
  badge?: string;
  /** Right-hand action area (buttons, status pills). */
  actions?: React.ReactNode;
  children: React.ReactNode;
}

export async function PortalShell({
  title,
  subtitle,
  nav,
  activeHref,
  badge,
  actions,
  children,
}: PortalShellProps) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* Top global chrome — slim, single-row, hairline border. The
          wordmark links home; the four primary tabs sit centred;
          theme-toggle anchors the right edge. Mirrors the landing
          SiteHeader exactly so a transition Landing -> /provide is
          visually seamless. */}
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
            <PortalNavLink href="/provide" active={activeHref?.startsWith("/provide")}>
              Provide
            </PortalNavLink>
            <PortalNavLink
              href="/customer"
              active={activeHref?.startsWith("/customer")}
            >
              Customer
            </PortalNavLink>
            <PortalNavLink href="/vpn" active={activeHref?.startsWith("/vpn")}>
              VPN
            </PortalNavLink>
            <PortalNavLink
              href="/account"
              active={activeHref?.startsWith("/account")}
            >
              Account
            </PortalNavLink>
          </nav>
          <div className="flex items-center gap-3">
            {/* Theme toggle lives at the right of the global header so
                it is reachable from every authenticated surface
                without duplicating it per-section. Client-side island. */}
            <ThemeToggle />
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-6xl px-6 py-10">
        <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
          <div>
            {badge ? (
              <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {badge}
              </p>
            ) : null}
            <h1 className="mt-1 text-3xl font-semibold tracking-tight text-foreground">
              {title}
            </h1>
            {subtitle ? (
              <p className="mt-2 max-w-2xl text-sm leading-relaxed text-muted-foreground">
                {subtitle}
              </p>
            ) : null}
          </div>
          {actions ? (
            <div className="flex flex-shrink-0 gap-2">{actions}</div>
          ) : null}
        </div>

        {nav.length > 0 ? (
          <nav
            aria-label="Section"
            className="mt-8 flex flex-wrap gap-1 border-b border-border"
          >
            {nav.map((item) => (
              <SectionTab key={item.href} item={item} active={activeHref === item.href} />
            ))}
          </nav>
        ) : null}

        <main className="mt-8">{children}</main>
      </div>
    </div>
  );
}

function PortalNavLink({
  href,
  active,
  children,
}: {
  href: string;
  active?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Link
      href={href}
      className={cn(
        "text-sm transition-colors",
        active
          ? "font-medium text-foreground"
          : "text-muted-foreground hover:text-foreground",
      )}
      aria-current={active ? "page" : undefined}
    >
      {children}
    </Link>
  );
}

function SectionTab({ item, active }: { item: NavItem; active?: boolean }) {
  return (
    <Link
      href={item.href}
      className={cn(
        "-mb-px border-b-2 px-3 py-2.5 text-sm transition-colors",
        active
          ? "border-foreground font-medium text-foreground"
          : "border-transparent text-muted-foreground hover:border-border-strong hover:text-foreground",
      )}
      aria-current={active ? "page" : undefined}
    >
      {item.label}
    </Link>
  );
}
