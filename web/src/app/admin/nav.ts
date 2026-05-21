import type { NavItem } from "@/components/layout/portal-shell";

export const ADMIN_NAV: NavItem[] = [
  { href: "/admin", label: "Overview" },
  { href: "/admin/abuse", label: "Abuse queue" },
  { href: "/admin/customers", label: "Customers" },
  { href: "/admin/providers", label: "Providers" },
];
