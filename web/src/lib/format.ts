/**
 * Display formatters shared by the provider + customer dashboards.
 * All functions are pure and locale-independent (Intl is fine in
 * Server Components — Next.js polyfills it).
 */

/**
 * Format a byte count using binary units. We always show 1 decimal so
 * the value stays stable as it grows ("1.4 GB" not "1 GB"→"2 GB").
 */
export function formatBytes(input: number | string): string {
  const n = typeof input === "string" ? Number(input) : input;
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let val = n;
  let unit = 0;
  while (val >= 1024 && unit < units.length - 1) {
    val /= 1024;
    unit += 1;
  }
  const digits = unit === 0 ? 0 : 1;
  return `${val.toFixed(digits)} ${units[unit]}`;
}

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

/**
 * Money amounts cross the wire as decimal strings to avoid float
 * rounding. Display them via Intl.NumberFormat for currency.
 */
export function formatMoney(
  amount: string | number | undefined,
  currencyCode = "USD",
): string {
  if (amount === undefined || amount === null || amount === "") return "—";
  const n = typeof amount === "string" ? Number(amount) : amount;
  if (!Number.isFinite(n)) return "—";
  try {
    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: currencyCode,
      maximumFractionDigits: 2,
    }).format(n);
  } catch {
    return `$${n.toFixed(2)}`;
  }
}

/** Format an EventKind enum value as a human-readable label. */
export function eventKindLabel(kind: string): string {
  switch (kind) {
    case "EVENT_KIND_WORKLOAD_DISPATCHED":
      return "Workload dispatched";
    case "EVENT_KIND_WORKLOAD_COMPLETED":
      return "Workload completed";
    case "EVENT_KIND_WORKLOAD_BLOCKED":
      return "Workload blocked";
    case "EVENT_KIND_SCHEDULER_TRANSITION":
      return "Scheduler change";
    case "EVENT_KIND_ABUSE_FLAGGED":
      return "Abuse flagged";
    case "EVENT_KIND_EARNINGS_CREDITED":
      return "Earnings credited";
    default:
      return "Event";
  }
}

/** Slugged category label → human-readable. */
export function categoryLabel(slug: string): string {
  if (!slug) return "General";
  return slug
    .split(/[_\-]/)
    .map((s) => s.charAt(0).toUpperCase() + s.slice(1).toLowerCase())
    .join(" ");
}

/** Map an event kind to a short single-letter glyph for the row icon. */
export function eventKindGlyph(kind: string): string {
  switch (kind) {
    case "EVENT_KIND_WORKLOAD_DISPATCHED":
      return ">";
    case "EVENT_KIND_WORKLOAD_COMPLETED":
      return "+";
    case "EVENT_KIND_WORKLOAD_BLOCKED":
      return "x";
    case "EVENT_KIND_SCHEDULER_TRANSITION":
      return "~";
    case "EVENT_KIND_ABUSE_FLAGGED":
      return "!";
    case "EVENT_KIND_EARNINGS_CREDITED":
      return "$";
    default:
      return "·";
  }
}
