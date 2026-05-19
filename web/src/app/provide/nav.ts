import type { NavItem } from "@/components/layout/portal-shell";

// The section nav for /provide/*. Defined in one place so the shell on
// every page renders the same tabs without re-declaring them.
export const PROVIDE_NAV: NavItem[] = [
  { href: "/provide", label: "Overview" },
  { href: "/provide/audit", label: "Transparency feed" },
  { href: "/provide/schedule", label: "Schedule" },
  { href: "/provide/earnings", label: "Earnings" },
];
