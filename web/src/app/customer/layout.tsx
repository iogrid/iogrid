import type { Metadata } from "next";
import { AppShell } from "@/components/layout/app-shell";

export const metadata: Metadata = {
  title: { template: "%s — iogrid Customer", default: "Customer" },
};

const ITEMS = [
  { href: "/customer",           label: "Overview" },
  { href: "/customer/workloads", label: "Workloads" },
  { href: "/customer/api-keys",  label: "API keys" },
  { href: "/customer/billing",   label: "Billing" },
  { href: "/customer/usage",     label: "Usage" },
];

export default function CustomerLayout({ children }: { children: React.ReactNode }) {
  return (
    <AppShell persona="customer" title="Customer" items={ITEMS}>
      {children}
    </AppShell>
  );
}
