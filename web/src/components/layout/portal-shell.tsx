import Link from "next/link";
import * as React from "react";
import { auth } from "@/lib/auth";
import { cn } from "@/lib/utils";

/**
 * PortalShell is the chrome shared by every authenticated app surface
 * (/provide, /customer, /account, /admin). It enforces a single layout
 * pattern so sub-agents and follow-up PRs cannot reintroduce bespoke
 * top-bars or sidebars.
 *
 * Server-component-safe (async). Interactive bits (user menu, sign-out)
 * live in dedicated client islands.
 *
 * The top-bar conditionally renders an "Admin" tab when the signed-in
 * user's email matches the `IOGRID_ADMIN_EMAILS` allowlist — the same
 * env-driven gate the middleware enforces on `/admin/*`. Non-admin
 * sessions see the original four tabs (Provide / Customer / VPN /
 * Account) unchanged.
 */

function isAdminEmail(email: string | null | undefined): boolean {
  if (!email) return false;
  const raw = process.env.IOGRID_ADMIN_EMAILS ?? "";
  const allow = new Set(
    raw
      .split(",")
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean),
  );
  return allow.has(email.toLowerCase());
}

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
  const session = await auth();
  const showAdminTab = isAdminEmail(session?.user?.email);

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
            {showAdminTab ? (
              <PortalNavLink
                href="/admin"
                active={activeHref?.startsWith("/admin")}
              >
                Admin
              </PortalNavLink>
            ) : null}
          </nav>
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
