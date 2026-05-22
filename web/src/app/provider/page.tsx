import Link from "next/link";
import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDER_NAV } from "@/app/provider/nav";
import { ProvideOverview } from "./overview";

export const metadata = {
  title: "Provider dashboard — iogrid",
};

/**
 * /provide — the operator-facing landing page. Status pill, today's
 * earnings counter, bandwidth-cap progress bar, recent audit events,
 * quick links into the sub-routes.
 */
export default function ProvideDashboardPage() {
  return (
    <PortalShell
      badge="Provider"
      title="Provider overview"
      subtitle="Your machine's contribution at a glance."
      nav={PROVIDER_NAV}
      activeHref="/provider"
      actions={
        <Link
          href="/install"
          className="rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background hover:bg-foreground/80 dark:bg-foreground dark:text-background dark:hover:bg-muted"
        >
          Install the daemon
        </Link>
      }
    >
      <ProvideOverview />
    </PortalShell>
  );
}
