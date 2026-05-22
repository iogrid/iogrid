import Link from "next/link";
import { Cpu, Boxes, ShieldCheck, UserCircle2, LogOut } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Icon rail — leftmost vertical strip on the post-auth shell.
 * Switches between the consumer virtual apps:
 *   • Provider  (/provider/*)
 *   • Customer  (/customer/*)
 *   • VPN       (/vpn/*)
 *   • Account   (/account/*, shared identity layer)
 *
 * Linear/Notion/Vercel pattern: persistent, icon-only, active item
 * marked with an accent fill + a dot indicator on the left edge.
 *
 * Admin app at admin.iogrid.org uses a separate rail (no persona
 * switcher — admin is a single operator context).
 */

export type ConsumerPersona = "provider" | "customer" | "vpn" | "account";

const PERSONAS: {
  id: ConsumerPersona;
  href: string;
  label: string;
  icon: React.ElementType;
}[] = [
  { id: "provider", href: "/provider", label: "Provider", icon: Cpu },
  { id: "customer", href: "/customer", label: "Customer", icon: Boxes },
  { id: "vpn",      href: "/vpn",      label: "VPN",      icon: ShieldCheck },
  { id: "account",  href: "/account",  label: "Account",  icon: UserCircle2 },
];

export function PersonaRail({ active }: { active: ConsumerPersona }) {
  return (
    <nav
      aria-label="Switch persona"
      className="flex h-screen w-16 flex-col items-center justify-between border-r border-border bg-card py-4"
    >
      {/* Logo — top anchor, links to marketing apex */}
      <Link
        href="/"
        aria-label="iogrid home"
        className="flex h-10 w-10 items-center justify-center rounded-md bg-primary-500 text-white font-bold transition hover:bg-primary-600"
      >
        ig
      </Link>

      {/* Persona stack */}
      <ul className="mt-6 flex flex-1 flex-col items-center gap-2 pt-6">
        {PERSONAS.map((p) => {
          const isActive = p.id === active;
          const Icon = p.icon;
          return (
            <li key={p.id} className="relative">
              {isActive && (
                <span
                  aria-hidden
                  className="absolute left-0 top-1/2 h-6 w-1 -translate-y-1/2 rounded-r-full bg-primary-500"
                />
              )}
              <Link
                href={p.href}
                title={p.label}
                aria-label={p.label}
                aria-current={isActive ? "page" : undefined}
                className={cn(
                  "flex h-10 w-10 items-center justify-center rounded-md transition",
                  isActive
                    ? "bg-primary-50 text-primary-700 dark:bg-primary-900 dark:text-primary-200"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                )}
              >
                <Icon className="h-5 w-5" aria-hidden />
              </Link>
            </li>
          );
        })}
      </ul>

      {/* Sign-out anchor, separated by divider */}
      <div className="mt-4 flex w-full flex-col items-center border-t border-border pt-4">
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
  );
}
