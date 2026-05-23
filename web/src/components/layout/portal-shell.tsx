import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * PortalShell — page-level header + content wrapper used INSIDE the
 * post-#422-revamp AppShell (icon rail + persona sidebar).
 *
 * 2026-05-23 BREAKING SHAPE CHANGE per EPIC #422 final form: this
 * component no longer renders the top-bar tabs or persona switcher.
 * Those concerns moved to:
 *   • PersonaSwitcher        — leftmost icon column, switches persona.
 *   • PersonaSidebar     — second column, section nav for the ACTIVE
 *                          persona only.
 *   • AppShell           — composes the two + wraps content.
 *
 * What PortalShell now does: render the page-level title + subtitle +
 * optional badge + optional right-aligned action slot. That's it.
 *
 * Existing pages pass `nav` and `activeHref` for back-compat; both
 * props are IGNORED (the persona sidebar owns nav). Keeping them in
 * the type so call sites don't all need a same-PR edit — they drop
 * the props naturally as we re-theme each page.
 */

export interface NavItem {
  href: string;
  label: string;
  description?: string;
}

export interface PortalShellProps {
  /** Page-level h1. */
  title: string;
  /** Optional subtitle / lead paragraph beneath the h1. */
  subtitle?: string;
  /** Small accent pill rendered above the title (e.g. "Provider"). */
  badge?: string;
  /** Right-aligned action slot — usually a primary CTA. */
  actions?: React.ReactNode;
  /** @deprecated PersonaSidebar owns nav now. Kept for back-compat. */
  nav?: NavItem[];
  /** @deprecated PersonaSidebar owns nav now. Kept for back-compat. */
  activeHref?: string;
  className?: string;
  children: React.ReactNode;
}

export function PortalShell({
  title,
  subtitle,
  badge,
  actions,
  className,
  children,
}: PortalShellProps) {
  return (
    <div className={cn("flex flex-col", className)}>
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
            {actions ? <div className="flex-shrink-0">{actions}</div> : null}
          </div>
        </div>
      </header>
      <div className="flex-1 px-8 py-8">{children}</div>
    </div>
  );
}
