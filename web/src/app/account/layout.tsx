import type { Metadata } from "next";
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

export default function AccountLayout({ children }: { children: React.ReactNode }) {
  return (
    <AppShell persona="account" title="Account" items={ITEMS}>
      {children}
    </AppShell>
  );
}
