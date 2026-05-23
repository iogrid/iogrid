"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Cpu, Boxes, ShieldCheck, UserCircle2, ChevronDown, LogOut } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Top-bar persona switcher dropdown — replaces the 16px PersonaRail
 * column. Founder feedback 2026-05-23 (#470): the dual left-pane
 * approach was visually noisy; switch to a header-mounted dropdown
 * so only ONE left pane (the section sub-nav) renders.
 *
 * Pattern: Linear's workspace switcher / Vercel's project switcher.
 * Click the button → menu with all 4 personas + Account + Sign out.
 * Keyboard: arrow keys + enter to navigate; Escape closes.
 */

export type ConsumerPersona = "provider" | "customer" | "vpn" | "account";

const PERSONAS: {
  id: ConsumerPersona;
  href: string;
  label: string;
  icon: React.ElementType;
  blurb: string;
}[] = [
  { id: "provider", href: "/provider", label: "Provider", icon: Cpu,         blurb: "Share idle hardware + bandwidth" },
  { id: "customer", href: "/customer", label: "Customer", icon: Boxes,       blurb: "Buy compute, proxy, iOS-build" },
  { id: "vpn",      href: "/vpn",      label: "VPN",      icon: ShieldCheck, blurb: "Unmetered private routing" },
  { id: "account",  href: "/account",  label: "Account",  icon: UserCircle2, blurb: "Profile, billing, sign out" },
];

export function PersonaSwitcher({ active }: { active: ConsumerPersona }) {
  const router = useRouter();
  const [open, setOpen] = React.useState(false);
  const buttonRef = React.useRef<HTMLButtonElement | null>(null);
  const menuRef = React.useRef<HTMLDivElement | null>(null);

  const current = PERSONAS.find((p) => p.id === active) ?? PERSONAS[0]!;
  const CurrentIcon = current.icon;

  // Click-outside + escape close.
  React.useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        buttonRef.current?.contains(e.target as Node)
      ) return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div className="relative">
      <button
        ref={buttonRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="inline-flex items-center gap-2 rounded-md border border-border bg-card px-3 py-1.5 text-sm font-medium text-foreground transition-colors hover:bg-muted"
        data-testid="persona-switcher-trigger"
      >
        <CurrentIcon className="h-4 w-4 text-primary-600" aria-hidden />
        <span>{current.label}</span>
        <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
      </button>

      {open ? (
        <div
          ref={menuRef}
          role="menu"
          aria-label="Switch persona"
          className="absolute left-0 top-full z-40 mt-2 w-72 rounded-md border border-border bg-card shadow-lg"
        >
          <ul className="p-1">
            {PERSONAS.map((p) => {
              const isActive = p.id === active;
              const Icon = p.icon;
              return (
                <li key={p.id}>
                  <Link
                    href={p.href}
                    role="menuitem"
                    onClick={() => setOpen(false)}
                    className={cn(
                      "flex items-start gap-3 rounded-sm px-3 py-2 text-sm transition-colors hover:bg-muted",
                      isActive && "bg-primary-50 dark:bg-primary-900/30",
                    )}
                    data-testid={`persona-switch-${p.id}`}
                  >
                    <Icon
                      className={cn(
                        "mt-0.5 h-4 w-4 shrink-0",
                        isActive ? "text-primary-700 dark:text-primary-300" : "text-muted-foreground",
                      )}
                      aria-hidden
                    />
                    <div className="min-w-0 flex-1">
                      <div
                        className={cn(
                          "font-medium",
                          isActive
                            ? "text-primary-700 dark:text-primary-300"
                            : "text-foreground",
                        )}
                      >
                        {p.label}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {p.blurb}
                      </div>
                    </div>
                    {isActive ? (
                      <span
                        aria-hidden
                        className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-primary-500"
                      />
                    ) : null}
                  </Link>
                </li>
              );
            })}
          </ul>
          <div className="border-t border-border p-1">
            <Link
              href="/api/auth/signout"
              role="menuitem"
              onClick={() => setOpen(false)}
              className="flex items-center gap-3 rounded-sm px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              data-testid="persona-switcher-signout"
            >
              <LogOut className="h-4 w-4" aria-hidden />
              Sign out
            </Link>
          </div>
        </div>
      ) : null}
    </div>
  );
}
