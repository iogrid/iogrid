import type { Metadata } from "next";
import { AppShell } from "@/components/layout/app-shell";

export const metadata: Metadata = {
  title: { template: "%s — iogrid Provider", default: "Provider" },
};

const ITEMS = [
  { href: "/provider",          label: "Overview" },
  { href: "/provider/audit",    label: "Transparency" },
  { href: "/provider/schedule", label: "Schedule" },
  { href: "/provider/earnings", label: "Earnings" },
  { href: "/provider/staking",  label: "Staking" },
];

export default function ProviderLayout({ children }: { children: React.ReactNode }) {
  return (
    <AppShell persona="provider" title="Provider" items={ITEMS}>
      {children}
    </AppShell>
  );
}
