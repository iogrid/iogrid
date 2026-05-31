import type { NavItem } from "@/components/layout/portal-shell";

export const CUSTOMER_NAV: NavItem[] = [
  { href: "/customer", label: "Overview" },
  { href: "/customer/api-keys", label: "API keys" },
  { href: "/customer/vpn", label: "VPN" },
  { href: "/customer/workloads", label: "Workloads" },
  { href: "/customer/usage", label: "Usage" },
  { href: "/customer/billing", label: "Billing" },
];
