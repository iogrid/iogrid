/**
 * Staking helpers — the on-chain stake program is custodied by
 * billing-svc, so the UI talks to the gateway-bff (not directly to
 * Solana). The endpoints below mirror docs/TOKENOMICS.md §Layer-3
 * provider lockup tiers + customer-side voluntary stake.
 *
 *   GET  /api/v1/staking/positions          → list active stakes
 *   POST /api/v1/staking/stake              → open a new position
 *   POST /api/v1/staking/claim              → claim accrued yield
 *   POST /api/v1/staking/early-unlock       → trigger 50% burn
 *
 * Tier configuration is also fetched server-side so the multiplier
 * table can be updated without a UI ship.
 */

import { ApiClient } from "@/lib/api";

/**
 * Lock-period selector. Maps 1:1 onto docs/TOKENOMICS.md's
 * Standard / Loyalty / Conviction / Maximum tiers.
 */
export type LockPeriodDays = 30 | 90 | 180 | 365;

export interface StakingTier {
  lockPeriodDays: LockPeriodDays;
  name: string;
  multiplier: number;
}

export const STAKING_TIERS: readonly StakingTier[] = [
  { lockPeriodDays: 30, name: "Standard", multiplier: 1.0 },
  { lockPeriodDays: 90, name: "Loyalty", multiplier: 1.25 },
  { lockPeriodDays: 180, name: "Conviction", multiplier: 1.5 },
  { lockPeriodDays: 365, name: "Maximum", multiplier: 2.0 },
];

export interface StakePosition {
  /** UUID minted by billing-svc. */
  id: string;
  /** Decimal $GRID amount, raw base-units string. */
  amount: string;
  /** Decimal $GRID amount UI form. */
  amountUi: number;
  lockPeriodDays: LockPeriodDays;
  tierMultiplier: number;
  /** ISO-8601 stake-open timestamp. */
  openedAt: string;
  /** ISO-8601 unlock timestamp. */
  unlocksAt: string;
  /** Accrued yield UI form. */
  accruedYieldUi: number;
  /** Whether the position is currently fully unlocked. */
  unlocked: boolean;
}

export interface ListPositionsResponse {
  positions: StakePosition[];
}

export interface StakeRequest {
  amount: string;
  lockPeriodDays: LockPeriodDays;
}

export async function listStakePositions(client: ApiClient): Promise<ListPositionsResponse> {
  return client.get<ListPositionsResponse>("/api/v1/staking/positions");
}

export async function openStakePosition(
  client: ApiClient,
  req: StakeRequest,
): Promise<StakePosition> {
  return client.post<StakePosition>("/api/v1/staking/stake", req);
}

export async function claimYield(
  client: ApiClient,
  positionId: string,
): Promise<StakePosition> {
  return client.post<StakePosition>("/api/v1/staking/claim", { positionId });
}

export async function earlyUnlock(
  client: ApiClient,
  positionId: string,
): Promise<{ position: StakePosition; burnedAmountUi: number }> {
  return client.post<{ position: StakePosition; burnedAmountUi: number }>(
    "/api/v1/staking/early-unlock",
    { positionId },
  );
}

/** Human-readable countdown like "12d 4h" for the unlocks-at field. */
export function formatRemainingLock(unlocksAt: string, nowMs = Date.now()): string {
  const ts = Date.parse(unlocksAt);
  if (!Number.isFinite(ts)) return "—";
  const ms = ts - nowMs;
  if (ms <= 0) return "Unlocked";
  const days = Math.floor(ms / 86_400_000);
  const hours = Math.floor((ms - days * 86_400_000) / 3_600_000);
  if (days > 0) return `${days}d ${hours}h`;
  const minutes = Math.floor((ms - hours * 3_600_000) / 60_000);
  return `${hours}h ${minutes}m`;
}

export function tierFor(period: LockPeriodDays): StakingTier {
  return (
    STAKING_TIERS.find((t) => t.lockPeriodDays === period) ?? STAKING_TIERS[0]
  );
}
