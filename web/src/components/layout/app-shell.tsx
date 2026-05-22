import { PersonaRail, type ConsumerPersona } from "./persona-rail";
import { PersonaSidebar, type SectionNavItem } from "./persona-sidebar";

/**
 * App shell — three-column layout used by every consumer virtual app
 * (provider/customer/vpn/account):
 *
 *   [icon rail] [persona sidebar] [content]
 *
 * Linear/Notion/Vercel pattern. The header text of the persona
 * sidebar (Provider / Customer / …) makes context unmistakable.
 * Switching personas = navigating from one virtual app to another
 * via the leftmost icon rail.
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
    <div className="flex min-h-screen bg-background text-foreground">
      <PersonaRail active={persona} />
      <PersonaSidebar title={title} items={items} />
      <main className="flex-1 overflow-y-auto">{children}</main>
    </div>
  );
}
