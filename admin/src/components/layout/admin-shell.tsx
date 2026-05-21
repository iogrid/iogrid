import Link from "next/link";
import * as React from "react";
import { auth } from "@/lib/auth";
import { cn } from "@/lib/utils";

/**
 * AdminShell is the chrome shared by every page in the admin/ app
 * (EPIC #422 Phase 1).
 *
 * Strict-separation invariant: this shell renders ONLY admin nav
 * items. It NEVER renders "Provide" / "Customer" / "VPN" tabs even
 * if a session somehow ends up here — those surfaces don't exist
 * inside this app. The founder's directive:
 *
 *   "admis app and user apps cannot be mixed to each other or
 *    instnace what is the point of showing the provide option to
 *    admin, he needs to access from teh eother indepent apps"
 *
 * If an admin who is also a provider needs to do provider work, they
 * navigate to iogrid.org (different host, different cookie, different
 * session) — that's the founder-blessed two-domain flow.
 *
 * Server-component-safe (async). The signed-in email is rendered in
 * the top bar as a visual "you are here" anchor; sign-out is a tiny
 * link to `/api/auth/signout`.
 *
 * Phase 2.3 of EPIC #422 will replace the visual identity (Linear /
 * Notion / Vercel premium-minimal aesthetic). The structural API of
 * this component — `title`, `subtitle`, `nav`, `activeHref` — should
 * stay stable across that redesign so per-route pages don't churn.
 */

export interface AdminNavItem {
  href: string;
  label: string;
  description?: string;
}

export interface AdminShellProps {
  title: string;
  subtitle?: string;
  nav: AdminNavItem[];
  activeHref?: string;
  badge?: string;
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
  const email = session?.user?.email ?? null;

  return (
    <div className="min-h-screen bg-background text-foreground dark:bg-foreground dark:text-background">
      <header className="border-b border-border bg-white dark:border-foreground dark:bg-foreground">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-6 py-3">
          <Link
            href="/"
            className="flex items-center gap-2 text-lg font-bold tracking-tight"
            aria-label="iogrid admin home"
          >
            <span>iogrid</span>
            <span className="rounded bg-foreground px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-white dark:bg-muted dark:text-foreground">
              admin
            </span>
          </Link>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            {email ? (
              <span className="hidden sm:inline" data-testid="admin-shell-email">
                {email}
              </span>
            ) : null}
            <Link
              href="/api/auth/signout"
              className="rounded-md border border-border px-2 py-1 text-foreground hover:bg-muted dark:border-foreground dark:text-muted-foreground dark:hover:bg-foreground"
            >
              Sign out
            </Link>
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-7xl px-6 py-8">
        <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
          <div>
            {badge ? (
              <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {badge}
              </p>
            ) : null}
            <h1 className="mt-1 text-3xl font-bold tracking-tight">{title}</h1>
            {subtitle ? (
              <p className="mt-1 text-sm text-foreground dark:text-muted-foreground">
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
            className="mt-6 flex flex-wrap gap-1 border-b border-border dark:border-foreground"
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

function SectionTab({
  item,
  active,
}: {
  item: AdminNavItem;
  active?: boolean;
}) {
  return (
    <Link
      href={item.href}
      className={cn(
        "border-b-2 px-3 py-2 text-sm font-medium",
        active
          ? "border-foreground text-foreground dark:border-muted dark:text-muted"
          : "border-transparent text-muted-foreground hover:border-border hover:text-foreground dark:hover:border-foreground dark:hover:text-border",
      )}
    >
      {item.label}
    </Link>
  );
}
