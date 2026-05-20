/**
 * Admin allowlist helpers — shared by the edge middleware and any
 * server-side guard that wants to verify an email belongs to the
 * IOGRID_ADMIN_EMAILS allowlist.
 *
 * The split exists so the middleware logic is unit-testable without
 * having to boot NextAuth + the edge runtime. The pure helpers below
 * are import-safe from both edge and node contexts.
 */

/**
 * Parse the comma-separated IOGRID_ADMIN_EMAILS env var into a
 * lowercase Set. Whitespace and empty entries are tolerated.
 */
export function parseAdminEmails(raw: string | undefined): Set<string> {
  const v = raw ?? "";
  return new Set(
    v
      .split(",")
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean),
  );
}

/**
 * True iff `email` (case-insensitive) is present in the allowlist
 * carried by `raw`.
 */
export function isAdminEmail(
  email: string | null | undefined,
  raw: string | undefined,
): boolean {
  if (!email) return false;
  return parseAdminEmails(raw).has(email.toLowerCase());
}
