/**
 * Display formatters shared by the provider + customer dashboards.
 * All functions are pure and locale-independent (Intl is fine in
 * Server Components — Next.js polyfills it).
 */

import {
  EventKindNames,
  WorkloadTypeNames,
  protoEnumName,
} from "@/lib/proto-enum";

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
 * Convert a `google.protobuf.Timestamp` as marshalled by Go's
 * `encoding/json` ({seconds, nanos}) into milliseconds since epoch.
 * Accepts an RFC3339 string fallback for when the BFF eventually
 * switches to `protojson`. Returns `null` if the value is missing or
 * represents "never observed" (seconds === 0).
 */
export function protoTimestampToMillis(
  ts:
    | { seconds?: string | number; nanos?: number }
    | string
    | null
    | undefined,
): number | null {
  if (ts == null) return null;
  if (typeof ts === "string") {
    const parsed = Date.parse(ts);
    return Number.isFinite(parsed) ? parsed : null;
  }
  const rawSeconds = ts.seconds;
  if (rawSeconds === undefined || rawSeconds === null) return null;
  const seconds =
    typeof rawSeconds === "string" ? Number(rawSeconds) : rawSeconds;
  if (!Number.isFinite(seconds) || seconds <= 0) return null;
  const nanos = Number.isFinite(ts.nanos) ? (ts.nanos as number) : 0;
  return Math.round(seconds * 1000 + nanos / 1_000_000);
}

/**
 * Relative-time formatter that accepts either a ProtoTimestamp
 * {seconds, nanos} or an RFC3339 string. "never observed" / missing
 * inputs render as `"never"` (not `"—"`) so the paired-machines card
 * (#318) gives operators a clear "daemon has not checked in yet"
 * signal instead of an em-dash that's overloaded with "unknown".
 */
export function formatProtoTimestampRelative(
  ts:
    | { seconds?: string | number; nanos?: number }
    | string
    | null
    | undefined,
  nowMs: number = Date.now(),
): string {
  const ms = protoTimestampToMillis(ts);
  if (ms === null) return "never";
  const iso = new Date(ms).toISOString();
  return formatRelativeTime(iso, nowMs);
}

/**
 * Format a ProtoTimestamp as a locale date string ("Mar 14, 2026").
 * Returns `"—"` when the timestamp is missing or zero.
 */
export function formatProtoTimestampAbsolute(
  ts:
    | { seconds?: string | number; nanos?: number }
    | string
    | null
    | undefined,
): string {
  const ms = protoTimestampToMillis(ts);
  if (ms === null) return "—";
  return new Date(ms).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/**
 * Truncate a long identifier (UUID, hash, etc.) middle-style:
 * "808ce330-79c1...5a94d8". Keeps `head` chars at the front and
 * `tail` chars at the end. Pure / side-effect free.
 */
export function truncateMiddle(
  value: string | undefined | null,
  head = 8,
  tail = 4,
): string {
  if (!value) return "";
  if (value.length <= head + tail + 1) return value;
  return `${value.slice(0, head)}…${value.slice(-tail)}`;
}

/**
 * Money amounts cross the wire as decimal strings to avoid float
 * rounding. Display them via Intl.NumberFormat for ISO-4217 currencies,
 * with a dedicated branch for the iogrid native token $GRID (which is
 * not ISO-4217 — Intl.NumberFormat would throw on it).
 *
 * Phase-0 empty-state contract (#312): when `currencyCode === "GRID"`
 * and `amount` is undefined / null / empty (proto3 omits int64 zero on
 * the wire, so `EarningsSummary.totalEarned.micros === 0` arrives as
 * `amount === undefined`), render `"0 $GRID"` — NOT `"—"`. The em-dash
 * is reserved for "value genuinely unfetchable" cases (e.g. the live
 * Solana wallet balance gated on #274).
 */
export function formatMoney(
  amount: string | number | undefined,
  currencyCode = "USD",
): string {
  const isGrid = currencyCode === "GRID";
  if (amount === undefined || amount === null || amount === "") {
    // For $GRID, an absent amount means "0 $GRID" (Phase-0 zero-workload
    // state, see #312). For ISO currencies, keep the legacy "—" so we
    // don't accidentally claim "$0.00" when the value is unknown.
    return isGrid ? "0 $GRID" : "—";
  }
  const n = typeof amount === "string" ? Number(amount) : amount;
  if (!Number.isFinite(n)) return isGrid ? "0 $GRID" : "—";
  if (isGrid) return formatGrid(n);
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

/**
 * Minimal structural Money: the proto3-JSON canonical shape is
 * `{ currency, micros }` (micros as a decimal string), but we tolerate the
 * legacy `{ currencyCode, amount }` shape too so a partial rollout never
 * crashes. See #633.
 */
type MoneyLike =
  | {
      currency?: string;
      micros?: string | number;
      currencyCode?: string;
      amount?: string | number;
    }
  | null
  | undefined;

/**
 * moneyCurrency returns the currency code from either the proto3-JSON
 * (`currency`) or the legacy (`currencyCode`) shape, defaulting to the
 * native token "GRID" (#312/#315).
 */
export function moneyCurrency(m: MoneyLike): string {
  if (!m) return "GRID";
  return m.currency ?? m.currencyCode ?? "GRID";
}

/**
 * moneyMajorUnits converts a Money to its major-unit numeric value
 * (e.g. micros "1500000" → 1.5). Reads the canonical `micros` field first
 * (int64 micros, 1 unit == 1_000_000), then falls back to the legacy
 * decimal-string `amount`. Returns undefined when no value is present so
 * formatMoney can render the Phase-0 "0 $GRID" / "—" empty-state. #633.
 */
export function moneyMajorUnits(m: MoneyLike): number | undefined {
  if (!m) return undefined;
  if (m.micros !== undefined && m.micros !== null && m.micros !== "") {
    const micros = typeof m.micros === "string" ? Number(m.micros) : m.micros;
    if (Number.isFinite(micros)) return micros / 1_000_000;
  }
  if (m.amount !== undefined && m.amount !== null && m.amount !== "") {
    const amt = typeof m.amount === "string" ? Number(m.amount) : m.amount;
    if (Number.isFinite(amt)) return amt;
  }
  return undefined;
}

/**
 * formatMoneyProto renders a proto Money (`{ currency, micros }`) for
 * display. This is the correct entry point for any Money that crosses the
 * gateway-bff protojson boundary — it reads `micros`, not the non-existent
 * `.amount`, so credited providers no longer render "0 $GRID" (#633).
 */
export function formatMoneyProto(m: MoneyLike): string {
  return formatMoney(moneyMajorUnits(m), moneyCurrency(m));
}

/**
 * Format a numeric $GRID amount with up to 4 fractional digits (the
 * token is 6-decimal on Solana but UI fidelity below 4dp is noise).
 * Trailing zeros are stripped so "1.0000 $GRID" renders as "1 $GRID"
 * and "0.5000" as "0.5 $GRID". Whole-token amounts get a thousands
 * separator so "$GRID balance: 12,345" is readable.
 */
function formatGrid(n: number): string {
  // Whole numbers — locale grouping, no decimals.
  if (Number.isInteger(n)) {
    return `${new Intl.NumberFormat("en-US").format(n)} $GRID`;
  }
  // Fractional — up to 4 decimals, trailing zeros trimmed.
  const fixed = n.toFixed(4);
  const trimmed = fixed.replace(/\.?0+$/, "");
  return `${trimmed} $GRID`;
}

/**
 * Format an EventKind enum value as a human-readable label.
 *
 * Accepts either the numeric proto tag (the form gateway-bff emits via
 * `encoding/json`) or the canonical SCREAMING_SNAKE_CASE name. See #314.
 */
export function eventKindLabel(kind: string | number): string {
  const name = protoEnumName(kind, EventKindNames);
  switch (name) {
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

/**
 * Map an event kind to a short single-letter glyph for the row icon.
 *
 * Accepts either numeric proto tag or canonical name. See #314.
 */
export function eventKindGlyph(kind: string | number): string {
  const name = protoEnumName(kind, EventKindNames);
  switch (name) {
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

/**
 * Format a WorkloadType enum value as a human-readable label.
 *
 * Accepts either numeric proto tag or canonical name. See #314.
 */
export function workloadTypeLabel(t: string | number): string {
  const name = protoEnumName(t, WorkloadTypeNames);
  switch (name) {
    case "WORKLOAD_TYPE_BANDWIDTH":
      return "Bandwidth";
    case "WORKLOAD_TYPE_DOCKER":
      return "Docker";
    case "WORKLOAD_TYPE_GPU":
      return "GPU";
    case "WORKLOAD_TYPE_IOS_BUILD":
      return "iOS build";
    default:
      return typeof t === "string" ? t : "Workload";
  }
}
