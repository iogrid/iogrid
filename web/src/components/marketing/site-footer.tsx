import Link from "next/link";

/**
 * Shared site footer for the public surface. Lifted out of
 * `web/src/app/page.tsx` as part of EPIC #422 Phase 3 — keeps the
 * about / pricing / legal / status pages consistent with the apex
 * landing page.
 */
export function SiteFooter() {
  return (
    <footer>
      <div className="mx-auto flex max-w-6xl flex-col items-start gap-4 px-6 py-10 text-xs text-muted-foreground md:flex-row md:items-center md:justify-between">
        <div className="flex items-center gap-2">
          <span className="font-semibold text-foreground">iogrid</span>
          <span aria-hidden>·</span>
          <span>Open-source. Operator-owned.</span>
        </div>
        <nav aria-label="Footer">
          <ul className="flex flex-wrap gap-x-6 gap-y-2">
            <li>
              <FooterLink href="/install">Install</FooterLink>
            </li>
            <li>
              <FooterLink href="/customer">Customer</FooterLink>
            </li>
            <li>
              <FooterLink href="/provide">Provider</FooterLink>
            </li>
            <li>
              <FooterLink href="/pricing">Pricing</FooterLink>
            </li>
            <li>
              <FooterLink href="/about">About</FooterLink>
            </li>
            <li>
              <FooterLink href="/legal/tos">Terms</FooterLink>
            </li>
            <li>
              <FooterLink href="/legal/privacy">Privacy</FooterLink>
            </li>
            <li>
              <FooterLink href="/legal/aup">AUP</FooterLink>
            </li>
            <li>
              <FooterLink href="/status">Status</FooterLink>
            </li>
            <li>
              <FooterLink href="https://github.com/iogrid/iogrid">
                GitHub
              </FooterLink>
            </li>
          </ul>
        </nav>
      </div>
    </footer>
  );
}

function FooterLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <Link href={href} className="transition-colors hover:text-foreground">
      {children}
    </Link>
  );
}
