/**
 * $GRID ↔ USD pricing helpers.
 *
 * iogrid's pitch is per-byte transparency, so the user should never have
 * to guess what they're paying. The top-up screen had the conversion
 * ratio (100 $GRID = $1) baked into three places — the balance line, the
 * quick-amount chips' hand-written `usd` fields, and nowhere for the
 * custom amount. Centralizing it here gives one source of truth, lets the
 * custom field + the Continue CTA show a live price, and makes the ratio
 * unit-testable (a silently-wrong money conversion is the worst kind of
 * regression).
 *
 * Pure — no React, no native imports — so it's covered by jest directly.
 *
 * Refs #580, #594.
 */

/** $GRID per US dollar. 1 $GRID = $0.01. */
export const GRID_PER_USD = 100;

/**
 * Convert a $GRID amount to its USD value. Non-finite or negative inputs
 * collapse to 0 — a top-up screen must never render "$NaN" or a negative
 * price, and the caller already gates real input through a number-pad.
 */
export function gridToUsd(grid: number): number {
  if (!Number.isFinite(grid) || grid <= 0) return 0;
  return grid / GRID_PER_USD;
}

/**
 * Format a $GRID amount as a `$X.XX` USD string for display. Always two
 * decimals (money), always non-negative.
 */
export function formatGridAsUsd(grid: number): string {
  return `$${gridToUsd(grid).toFixed(2)}`;
}
