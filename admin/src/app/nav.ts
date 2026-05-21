import type { AdminNavItem } from "@/components/layout/admin-shell";

/**
 * Canonical admin nav for admin.iogrid.org (EPIC #422 Phase 1).
 *
 * Surfaces are admin-only by definition: every entry here exists for
 * staff (provider pool audit, abuse review, billing audit, system
 * health). The strict-separation invariant: NEVER add a /provide,
 * /customer, or /vpn entry — those live on iogrid.org (user-facing
 * app), and an admin who also acts as a provider hits that other
 * host with a separate session.
 *
 * /billing and /health are stub-shipped in Phase 1 so the nav looks
 * complete; the panels themselves get fleshed out as the supporting
 * BFF routes ship (tracked separately).
 */
export const ADMIN_NAV: AdminNavItem[] = [
  { href: "/", label: "Overview" },
  { href: "/providers", label: "Providers" },
  { href: "/abuse", label: "Abuse queue" },
  { href: "/billing", label: "Billing" },
  { href: "/health", label: "Health" },
];
