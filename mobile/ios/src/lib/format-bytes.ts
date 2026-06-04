/**
 * Human-readable byte formatting for the live-session traffic counters
 * (the ↓/↑ rows on the connected Main screen — TC-25).
 *
 * Extracted from index.tsx so the boundary logic (B → KB → MB → GB, and
 * the per-unit decimal precision) is unit-tested rather than eyeballed:
 * an off-by-one on a 1024 threshold or a wrong toFixed silently mis-states
 * how much data the user has pushed through their tunnel.
 *
 * Pure, no imports — covered by jest directly.
 *
 * Refs #580.
 */

/**
 * Format a byte count as `B` / `KB` / `MB` / `GB`. Sub-KB shows whole
 * bytes; KB/MB use one decimal; GB uses two. Non-finite or negative
 * inputs collapse to `0 B` — a traffic counter must never render
 * `NaN B` or a negative transfer (the native stats are always ≥0, but
 * the UI should be defensive about a missing/garbage stat frame).
 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
