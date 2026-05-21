/**
 * Display formatters shared across the admin app.
 * Slim copy of `web/src/lib/format.ts` — only `formatRelativeTime` is
 * used by the staff console today.
 */

/**
 * Convert an ISO timestamp into a "12s ago" style relative string.
 * Caller is responsible for re-rendering — the function is pure.
 */
export function formatRelativeTime(
  iso: string | undefined,
  nowMs: number = Date.now(),
): string {
  if (!iso) return "—";
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return "—";
  const dSec = Math.max(0, Math.round((nowMs - then) / 1000));
  if (dSec < 5) return "just now";
  if (dSec < 60) return `${dSec}s ago`;
  const dMin = Math.round(dSec / 60);
  if (dMin < 60) return `${dMin}m ago`;
  const dHr = Math.round(dMin / 60);
  if (dHr < 24) return `${dHr}h ago`;
  const dDay = Math.round(dHr / 24);
  if (dDay < 30) return `${dDay}d ago`;
  return new Date(then).toLocaleDateString();
}
