import { PortalShell } from "@/components/layout/portal-shell";
import { ACCOUNT_NAV } from "@/app/account/nav";
import { NotificationsPanel } from "./panel";

export const metadata = { title: "Notifications — iogrid" };

/**
 * /account/notifications — per-user notification-channel preferences
 * (issue #631). Lets the signed-in user choose, per event category,
 * whether iogrid emails them and/or shows an in-app notification:
 *
 *  - earnings credited
 *  - payout sent
 *  - security alerts
 *  - product updates
 *
 * Preferences persist server-side in identity-svc's
 * users.notification_prefs JSONB column (NOT localStorage). The page is
 * backed by gateway-bff's GET/POST /api/v1/account/notifications routes,
 * which forward to identity-svc via the service-token shim.
 */
export default function AccountNotificationsPage() {
  return (
    <PortalShell
      badge="Account"
      title="Notifications"
      subtitle="Choose which events iogrid emails you about and shows in-app — saved to your account, on every device."
      nav={ACCOUNT_NAV}
      activeHref="/account/notifications"
    >
      <NotificationsPanel />
    </PortalShell>
  );
}
