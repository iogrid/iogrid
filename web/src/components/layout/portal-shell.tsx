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
    <div className="min-h-screen bg-zinc-50 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-50">
      <header className="border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-3">
          <Link
            href="/"
            className="text-lg font-bold tracking-tight"
            aria-label="iogrid home"
          >
            iogrid
          </Link>
          <div className="flex items-center gap-2">
            <nav aria-label="Primary" className="hidden gap-2 md:flex">
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
            {/* Theme toggle lives at the right of the global header
                so it is reachable from every authenticated surface
                (provide / customer / vpn / account) without
                duplicating it per-section. Client-side island. */}
            <ThemeToggle className="ml-2" />
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-7xl px-6 py-8">
        <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
          <div>
            {badge ? (
              <p className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
                {badge}
              </p>
            ) : null}
            <h1 className="mt-1 text-3xl font-bold tracking-tight">{title}</h1>
            {subtitle ? (
              <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
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
            className="mt-6 flex flex-wrap gap-1 border-b border-zinc-200 dark:border-zinc-800"
          >
            {nav.map((item) => (
              <SectionTab key={item.href} item={item} active={activeHref === item.href} />
            ))}
          </nav>
        ) : null}

        <main className="mt-6">{children}</main>
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
        "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
        active
          ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900"
          : "text-zinc-600 hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800",
      )}
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
        "border-b-2 px-3 py-2 text-sm font-medium",
        active
          ? "border-zinc-900 text-zinc-900 dark:border-zinc-100 dark:text-zinc-100"
          : "border-transparent text-zinc-500 hover:border-zinc-300 hover:text-zinc-700 dark:hover:border-zinc-700 dark:hover:text-zinc-300",
      )}
    >
      {item.label}
    </Link>
  );
}
