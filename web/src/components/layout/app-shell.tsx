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
 *   │ [ig] [Persona ▾]                          [theme]   │   ← h-14
 *   ├──────────────┬──────────────────────────────────────┤
 *   │ section      │ content                              │
 *   │ sub-nav      │                                      │
 *   │              │                                      │
 *   └──────────────┴──────────────────────────────────────┘
 *
 * Founder picked Option A (#470): persona switch in the header
 * dropdown, single left pane for section sub-nav. Pattern: Linear
 * workspace switcher / Vercel project switcher.
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
