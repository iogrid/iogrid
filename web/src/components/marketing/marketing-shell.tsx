import { SiteHeader } from "./site-header";
import { SiteFooter } from "./site-footer";

/**
 * Wraps a public/marketing-style page (about, pricing, legal, status)
 * with the same header and footer as the apex landing.
 *
 * Use this for any page that should look like the landing page —
 * authenticated app surfaces (account, provide, customer, vpn) use
 * their own dashboard chrome and SHOULD NOT wrap with this.
 *
 * Folded into web/ from the deleted marketing/ workspace as part of
 * EPIC #422 Phase 3.
 */
export function MarketingShell({ children }: { children: React.ReactNode }) {
  return (
    <main className="min-h-screen bg-background text-foreground">
      <SiteHeader />
      {children}
      <SiteFooter />
    </main>
  );
}
