import type { NavItem } from "@/components/layout/portal-shell";

// The section nav for /provider/*. Defined in one place so the shell on
// every page renders the same tabs without re-declaring them.
export const PROVIDER_NAV: NavItem[] = [
  { href: "/provider", label: "Overview" },
  { href: "/provider/audit", label: "Transparency feed" },
  { href: "/provider/schedule", label: "Schedule" },
  { href: "/provider/earnings", label: "Earnings" },
  { href: "/provider/staking", label: "Staking" },
];
