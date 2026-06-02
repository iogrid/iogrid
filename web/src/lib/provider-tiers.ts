/**
 * Provider earnings-lockup tiers — canonical mirror of whitepaper.md
 * §7.2 "Lockup tiers" + §7.1 "Base lockup". The whitepaper is the
 * source of truth for the cliff + linear-vest periods; this module
 * carries them as typed constants so the /provider/staking surface can
 * render the tier ladder + the rolling per-payout vesting schedule
 * WITHOUT inventing per-account numbers (issue #634, #417 anti-fake
 * guardrail).
 *
 * IMPORTANT: every $GRID payout is auto-locked at receipt under the
 * `vesting` Anchor program. The cliff + vest below describe the SHAPE
 * of each rolling lockup clock — not a single account-wide balance.
 * There is no backend endpoint yet that returns a provider's live
 * vesting positions (gateway-bff /api/v1/staking/* is a Phase-0 empty
 * stub), so the page renders these as informational mechanics, gated by
 * the real opt-in / tier state when it exists.
 */

/** A provider's chosen (or default) earnings-lockup tier. */
export type ProviderTierName = "Standard" | "Loyalty" | "Conviction" | "Maximum";

export interface ProviderTier {
  name: ProviderTierName;
  /** Cliff duration in days — 0% vested before this elapses. */
  cliffDays: number;
  /** Linear-vest duration in days after the cliff (0% → 100%). */
  vestDays: number;
  /** Rewards multiplier applied at the emission::distribute_to step. */
  multiplier: number;
  /** One-line description of who this tier suits. */
  blurb: string;
}

/**
 * The four lockup tiers, exactly as whitepaper.md §7.2 defines them.
 * Standard is the mandatory base lockup (§7.1) every provider gets;
 * the rest are opt-in upgrades that ratchet upward only (no downgrade).
 */
export const PROVIDER_TIERS: readonly ProviderTier[] = [
  {
    name: "Standard",
    cliffDays: 30,
    vestDays: 60,
    multiplier: 1.0,
    blurb: "Mandatory base lockup applied to every payout.",
  },
  {
    name: "Loyalty",
    cliffDays: 90,
    vestDays: 180,
    multiplier: 1.25,
    blurb: "Longer hold, 1.25× effective work weight.",
  },
  {
    name: "Conviction",
    cliffDays: 180,
    vestDays: 365,
    multiplier: 1.5,
    blurb: "Half-year cliff, 1.5× rewards on the same workload.",
  },
  {
    name: "Maximum",
    cliffDays: 365,
    vestDays: 730,
    multiplier: 2.0,
    blurb: "One-year cliff, top 2.0× multiplier — for max conviction.",
  },
];

/** Default tier every provider is on before opting up. */
export const DEFAULT_TIER: ProviderTier = PROVIDER_TIERS[0];

/** Look up a tier by name, falling back to Standard. */
export function tierByName(name: string | null | undefined): ProviderTier {
  return PROVIDER_TIERS.find((t) => t.name === name) ?? DEFAULT_TIER;
}
