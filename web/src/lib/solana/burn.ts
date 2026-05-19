/**
 * Public burn dashboard data fetching.
 *
 * The gateway-bff exposes anonymous-readable endpoints:
 *
 *   GET /api/v1/burn/summary
 *     → { totalBurnedUi: number, totalBurnedRaw: string, lastBurnAt: string }
 *
 *   GET /api/v1/burn/daily?days=30
 *     → { points: [{ date: "2026-05-19", burnedUi: number }] }
 *
 *   GET /api/v1/burn/events?limit=20
 *     → { events: [{ id, timestamp, amountUi, txSignature }] }
 *
 * The dashboard is rendered at `/burn` (no auth). The page reads only,
 * never mutates.
 */

import { ApiClient } from "@/lib/api";

export interface BurnSummary {
  /** Cumulative $GRID burned, UI form. */
  totalBurnedUi: number;
  /** Raw base-units string. */
  totalBurnedRaw: string;
  /** ISO-8601 of most recent burn event, optional. */
  lastBurnAt?: string;
}

export interface BurnDailyPoint {
  /** ISO date (YYYY-MM-DD) bucket. */
  date: string;
  burnedUi: number;
}

export interface BurnDailyResponse {
  points: BurnDailyPoint[];
}

export interface BurnEvent {
  id: string;
  timestamp: string;
  amountUi: number;
  /** Solana mainnet tx signature (base58). */
  txSignature: string;
  /** Optional source label (e.g. "buy-and-burn", "early-unlock"). */
  source?: string;
}

export interface BurnEventsResponse {
  events: BurnEvent[];
}

export async function fetchBurnSummary(client: ApiClient): Promise<BurnSummary> {
  return client.get<BurnSummary>("/api/v1/burn/summary");
}

export async function fetchBurnDaily(
  client: ApiClient,
  days = 30,
): Promise<BurnDailyResponse> {
  return client.get<BurnDailyResponse>(`/api/v1/burn/daily?days=${days}`);
}

export async function fetchBurnEvents(
  client: ApiClient,
  limit = 20,
): Promise<BurnEventsResponse> {
  return client.get<BurnEventsResponse>(`/api/v1/burn/events?limit=${limit}`);
}
