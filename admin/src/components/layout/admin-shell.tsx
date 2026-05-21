import Link from "next/link";
import * as React from "react";
import { auth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { ThemeToggle } from "@/components/theme-toggle";

/**
 * AdminShell — chrome shared by every page in the admin app.
 *
 * Slim sibling of `web/src/components/layout/portal-shell.tsx`. The
 * admin app only has the staff section, so the top-bar nav is just the
 * admin sub-routes (Abuse / Customers / Providers / Finops / Settings).
 * The web app's Provide/Customer/VPN tabs are intentionally absent —
 * this is admin.iogrid.org, a separate origin for a separate audience.
 *
 * Server-component-safe (async). Interactive bits (theme toggle, future
 * user menu) live in dedicated client islands.
 */

export interface NavItem {
  href: string;
  label: string;
  description?: string;
}

export interface AdminShellProps {
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

export async function AdminShell({
  title,
  subtitle,
  nav,
  activeHref,
  badge,
  actions,
  children,
}: AdminShellProps) {
  const session = await auth();
  const userEmail = session?.user?.email ?? null;

  return (
    <div className="min-h-screen bg-zinc-50 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-50">
      <header className="border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-3">
          <Link
            href="/"
            className="flex items-baseline gap-2 text-lg font-bold tracking-tight"
            aria-label="iogrid admin home"
          >
            <span>iogrid</span>
            <span className="rounded bg-zinc-900 px-1.5 py-0.5 text-xs font-semibold uppercase tracking-wide text-white dark:bg-zinc-100 dark:text-zinc-900">
              admin
            </span>
          </Link>
          <div className="flex items-center gap-3">
            {userEmail ? (
              <span
                className="hidden text-xs text-zinc-500 sm:inline"
                aria-label="Signed-in admin email"
              >
                {userEmail}
              </span>
            ) : null}
            <ThemeToggle />
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
              <SectionTab
                key={item.href}
                item={item}
                active={activeHref === item.href}
              />
            ))}
          </nav>
        ) : null}

        <main className="mt-6">{children}</main>
      </div>
    </div>
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
