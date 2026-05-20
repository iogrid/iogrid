import type { NavItem } from "@/components/layout/admin-shell";

/**
 * Top-level admin navigation. Lives at root paths inside the admin app
 * (no `/admin/` prefix — the admin app IS the staff console, and a
 * separate origin from app.iogrid.org).
 */
export const ADMIN_NAV: NavItem[] = [
  { href: "/", label: "Overview" },
  { href: "/abuse", label: "Abuse queue" },
  { href: "/customers", label: "Customers" },
  { href: "/providers", label: "Providers" },
  { href: "/finops", label: "Finops" },
  { href: "/settings", label: "Settings" },
];
