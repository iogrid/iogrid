import Link from "next/link";
import { ThemeToggle } from "@/components/theme-toggle";

/**
 * Shared site header for the public surface (landing, about, pricing,
 * legal, status). Lifted out of `web/src/app/page.tsx` as part of
 * EPIC #422 Phase 3 — the marketing/ workspace was folded into web/
 * and these public pages all share the same top navigation as the
 * landing page so the surface stays visually unified.
 *
 * Pure Server Component; the only client island is <ThemeToggle/>.
 */
export function SiteHeader() {
  return (
    <header className="border-b border-border">
      <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-6">
        <Link
          href="/"
          className="text-sm font-semibold tracking-tight"
          aria-label="iogrid home"
        >
          iogrid
        </Link>
        <nav aria-label="Primary" className="hidden items-center gap-6 md:flex">
          <HeaderLink href="/provide">Provide</HeaderLink>
          <HeaderLink href="/customer">Customer</HeaderLink>
          <HeaderLink href="/vpn">VPN</HeaderLink>
          <HeaderLink href="/pricing">Pricing</HeaderLink>
          <HeaderLink href="/about">About</HeaderLink>
          <HeaderLink href="/account">Account</HeaderLink>
        </nav>
        <div className="flex items-center gap-3">
          <ThemeToggle />
          <Link
            href="/install"
            className="hidden rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 sm:inline-flex"
          >
            Get iogrid
          </Link>
        </div>
      </div>
    </header>
  );
}

function HeaderLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <Link
      href={href}
      className="text-sm text-muted-foreground transition-colors hover:text-foreground"
    >
      {children}
    </Link>
  );
}
