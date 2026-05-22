import Link from "next/link";
import * as React from "react";
import { LogOut, ShieldCheck } from "lucide-react";
import { auth } from "@/lib/auth";
import { cn } from "@/lib/utils";

/**
 * AdminShell — Linear/Notion/Vercel left-rail chrome for the admin/
 * Next.js app (admin.iogrid.org).
 *
 * EPIC #422 final form: the consumer apps live at iogrid.org/* and
 * use PersonaRail + PersonaSidebar + AppShell (rail for persona switch,
 * sidebar for section nav). Admin is its own host with its own cookie
 * scope — operators ≤3, no persona switching — so the rail is a single
 * fixed column and the sidebar holds admin section nav.
 *
 * Strict-separation invariant: this shell renders ONLY admin nav
 * items. It NEVER renders Provider / Customer / VPN entries even if a
 * session somehow lands here — those surfaces live on iogrid.org
 * (different cookie scope).
 *
 * API: title / subtitle / nav / activeHref / badge / actions / children.
 * Kept stable from the Phase-1 horizontal-tab variant so per-route
 * pages don't churn during the visual revamp.
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
    <div className="flex min-h-screen bg-background text-foreground">
      {/* Leftmost rail — single fixed column with the admin logo + a
          sign-out anchor. No persona switcher (admin is a single
          operator context per the founder directive). */}
      <nav
        aria-label="Admin"
        className="flex h-screen w-16 flex-col items-center justify-between border-r border-border bg-card py-4"
      >
        <Link
          href="/"
          aria-label="iogrid admin home"
          className="flex h-10 w-10 items-center justify-center rounded-md bg-primary-500 font-bold text-white transition hover:bg-primary-600"
        >
          <ShieldCheck className="h-5 w-5" aria-hidden />
        </Link>

        <div className="flex flex-col items-center border-t border-border pt-4">
          <Link
            href="/api/auth/signout"
            title="Sign out"
            aria-label="Sign out"
            className="flex h-10 w-10 items-center justify-center rounded-md text-muted-foreground transition hover:bg-muted hover:text-foreground"
          >
            <LogOut className="h-5 w-5" aria-hidden />
          </Link>
        </div>
      </nav>

      {/* Section sidebar — admin items only. */}
      {nav.length > 0 ? (
        <aside
          aria-label="Section navigation"
          className="flex h-screen w-56 flex-col border-r border-border bg-card"
        >
          <div className="border-b border-border px-5 py-4">
            <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Admin
            </p>
            {email ? (
              <p
                className="mt-1 truncate text-xs text-muted-foreground"
                data-testid="admin-shell-email"
                title={email}
              >
                {email}
              </p>
            ) : null}
          </div>
          <ul className="flex flex-1 flex-col gap-1 p-3">
            {nav.map((item) => {
              const isActive = activeHref === item.href;
              return (
                <li key={item.href}>
                  <Link
                    href={item.href}
                    aria-current={isActive ? "page" : undefined}
                    className={cn(
                      "block rounded-md px-3 py-2 text-sm font-medium transition",
                      isActive
                        ? "bg-primary-50 text-primary-700 dark:bg-primary-900 dark:text-primary-200"
                        : "text-muted-foreground hover:bg-muted hover:text-foreground",
                    )}
                  >
                    {item.label}
                  </Link>
                </li>
              );
            })}
          </ul>
        </aside>
      ) : null}

      {/* Page content — header band + main content. */}
      <main className="flex-1 overflow-y-auto">
        <header className="border-b border-border bg-card">
          <div className="px-8 py-6">
            <div className="flex items-start justify-between gap-6">
              <div>
                {badge ? <p className="pill mb-3">{badge}</p> : null}
                <h1 className="text-2xl font-bold tracking-tight text-foreground md:text-3xl">
                  {title}
                </h1>
                {subtitle ? (
                  <p className="mt-2 max-w-2xl text-sm leading-relaxed text-muted-foreground">
                    {subtitle}
                  </p>
                ) : null}
              </div>
              {actions ? (
                <div className="flex-shrink-0 flex gap-2">{actions}</div>
              ) : null}
            </div>
          </div>
        </header>
        <div className="px-8 py-8">{children}</div>
      </main>
    </div>
  );
}
