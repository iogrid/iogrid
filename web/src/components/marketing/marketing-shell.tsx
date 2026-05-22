import { Nav } from "@/components/marketing-rich/Nav";
import { Footer } from "@/components/marketing-rich/Footer";

/**
 * Wraps every public marketing surface (landing, /pricing, /vpn, /compute,
 * /gpu, /proxy, /ios-build, /providers, /token, /docs, /blog, /transparency,
 * /about, /status, /legal/*) with the polished top nav + footer restored
 * from the pre-#428 marketing/ workspace.
 *
 * Authenticated app surfaces (/provider, /customer, /vpn account panel,
 * /account, /admin) use AppShell + PersonaRail + PersonaSidebar instead.
 */
export function MarketingShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <Nav />
      <main>{children}</main>
      <Footer />
    </div>
  );
}
