"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

/**
 * Second-column persona sidebar — shows section nav for the ACTIVE
 * virtual app only. No leakage from other personas (the rule from
 * founder direction 2026-05-22: "we cannot dump everything in one
 * page, current tab over tab approach is super confusing").
 *
 * The header text (Provider / Customer / VPN / Account) makes the
 * context unmistakable to the user.
 */

export interface SectionNavItem {
  href: string;
  label: string;
}

export function PersonaSidebar({
  title,
  items,
}: {
  title: string;
  items: SectionNavItem[];
}) {
  const pathname = usePathname();
  return (
    <aside
      aria-label={`${title} navigation`}
      // h-full not h-screen — AppShell now puts a top header (h-14) above
      // this aside, so h-screen would overflow the parent flex. Refs #470.
      className="flex h-full w-56 flex-col border-r border-border bg-card"
    >
      <div className="border-b border-border px-5 py-4">
        <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          {title}
        </p>
      </div>
      <ul className="flex flex-1 flex-col gap-1 p-3">
        {items.map((it) => {
          const isActive =
            pathname === it.href ||
            (it.href !== "/" + title.toLowerCase() &&
              pathname.startsWith(it.href + "/"));
          return (
            <li key={it.href}>
              <Link
                href={it.href}
                aria-current={isActive ? "page" : undefined}
                className={cn(
                  "block rounded-md px-3 py-2 text-sm font-medium transition",
                  isActive
                    ? "bg-primary-50 text-primary-700 dark:bg-primary-900 dark:text-primary-200"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                {it.label}
              </Link>
            </li>
          );
        })}
      </ul>
    </aside>
  );
}
