import type { Metadata } from "next";
import { auth } from "@/lib/auth";
import { AppShell } from "@/components/layout/app-shell";

export const metadata: Metadata = {
  title: { template: "%s — iogrid Account", default: "Account" },
};

const ITEMS = [
  { href: "/account",              label: "Profile" },
  { href: "/account/identifiers",  label: "Identifiers" },
  { href: "/account/wallets",      label: "Wallets" },
  { href: "/account/sessions",     label: "Sessions" },
  { href: "/account/notifications",label: "Notifications" },
  { href: "/account/danger-zone",  label: "Danger zone" },
];

/**
 * Unauthenticated users land on /account directly — both from raw
 * navigation and from the middleware's redirect off /provider, /customer.
 * Showing them PersonaRail + PersonaSidebar (with Provider / Customer /
 * VPN entries pointing into auth-gated surfaces) makes the sign-in
 * surface noisier and breaks keyboard-tab order to the email field.
 *
 * Branch: signed-out → render the sign-in surface raw; signed-in →
 * wrap in AppShell so the four-persona rail + Account sidebar appears.
 */
export default async function AccountLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = await auth();
  if (!session?.user) {
    return <>{children}</>;
  }
  return (
    <AppShell persona="account" title="Account" items={ITEMS}>
      {children}
    </AppShell>
  );
}
