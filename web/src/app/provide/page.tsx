import Link from "next/link";
import { PortalShell } from "@/components/layout/portal-shell";
import { PROVIDE_NAV } from "@/app/provide/nav";
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
      nav={PROVIDE_NAV}
      activeHref="/provide"
      actions={
        <Link
          href="/install"
          className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          Install the daemon
        </Link>
      }
    >
      <ProvideOverview />
    </PortalShell>
  );
}
