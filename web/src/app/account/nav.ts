import type { NavItem } from "@/components/layout/portal-shell";

export const ACCOUNT_NAV: NavItem[] = [
  { href: "/account", label: "Profile" },
  { href: "/account/identifiers", label: "Identifiers" },
  { href: "/account/sessions", label: "Sessions" },
  { href: "/account/danger-zone", label: "Danger zone" },
];
