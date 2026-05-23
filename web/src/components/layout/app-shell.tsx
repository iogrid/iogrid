import Link from "next/link";
import { ThemeToggle } from "@/components/theme-toggle";
import {
  PersonaSwitcher,
  type ConsumerPersona,
} from "./persona-switcher";
import { PersonaSidebar, type SectionNavItem } from "./persona-sidebar";

export type { ConsumerPersona };

/**
 * App shell — two-column layout used by every consumer virtual app
 * (provider/customer/vpn/account).
 *
 *   ┌─────────────────────────────────────────────────────┐
 *   │ [ig] [Persona ▾]              [theme]               │   ← top header (h-14)
 *   ├──────────────┬──────────────────────────────────────┤
 *   │ section      │ content                              │
 *   │ sub-nav      │                                      │
 *   │              │                                      │
 *   └──────────────┴──────────────────────────────────────┘
 *
 * Founder feedback 2026-05-23 (#470): the prior 3-column shape (icon
 * rail + persona sidebar + content) was visually noisy. Replaced
 * with header-mounted PersonaSwitcher dropdown. Pattern: Linear's
 * workspace switcher / Vercel's project switcher / Notion's
 * sidebar-top selector. Single left pane is now just the section
 * sub-nav within the active persona.
 */
export function AppShell({
  persona,
  title,
  items,
  children,
}: {
  persona: ConsumerPersona;
  title: string;
  items: SectionNavItem[];
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-screen flex-col bg-background text-foreground">
      <header className="flex h-14 items-center justify-between border-b border-border bg-card px-4">
        <div className="flex items-center gap-3">
          <Link
            href="/"
            aria-label="iogrid home"
            className="flex h-8 w-8 items-center justify-center rounded-md bg-primary-500 text-sm font-bold text-white transition hover:bg-primary-600"
          >
            ig
          </Link>
          <PersonaSwitcher active={persona} />
        </div>
        <div className="flex items-center gap-2">
          <ThemeToggle />
        </div>
      </header>
      <div className="flex flex-1 overflow-hidden">
        <PersonaSidebar title={title} items={items} />
        <main className="flex-1 overflow-y-auto">{children}</main>
      </div>
    </div>
  );
}
