import { AdminShell } from "@/components/layout/admin-shell";
import { ADMIN_NAV } from "@/app/(authed)/nav";

export const metadata = { title: "Settings — iogrid admin" };

/**
 * /settings — per-admin preferences (theme is already on the global
 * top-bar; future panels: notification routing, on-call rotation,
 * approval-quorum thresholds). Placeholder today so the admin nav
 * doesn't 404 against a visible tab.
 */
export default function AdminSettingsPage() {
  return (
    <AdminShell
      badge="Admin"
      title="Settings"
      subtitle="Operator preferences and per-admin configuration."
      nav={ADMIN_NAV}
      activeHref="/settings"
    >
      <div className="rounded-md border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900/40 dark:text-zinc-300">
        Admin-side configuration surfaces (notification routing, on-call
        rotation, approval-quorum thresholds) will land here as the
        operator workflow matures. The theme toggle in the global top-bar
        is the only per-admin preference today.
      </div>
    </AdminShell>
  );
}
