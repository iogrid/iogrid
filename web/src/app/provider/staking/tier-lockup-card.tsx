"use client";

/**
 * TierLockupCard — surfaces the provider's $GRID earnings-lockup tier +
 * the rolling per-payout vesting schedule (whitepaper §7). Two honest
 * states (issue #634 / #417 anti-fake guardrail):
 *
 *   - When the staking backend reports a real opted-in tier, we
 *     highlight it in the ladder + show the staked-$GRID headline.
 *   - When the backend is the Phase-0 empty stub (opted_in:false,
 *     stake_amount:0 — gateway-bff routes.go emptyStakingState), we do
 *     NOT fabricate a staked balance or a fake vest %. We render the
 *     mandatory Standard tier as the active row + the tier ladder as a
 *     "what you'd get if you upgrade" reference, with an explicit
 *     "No live vesting positions yet" note.
 */

import * as React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import {
  PROVIDER_TIERS,
  tierByName,
  type ProviderTier,
} from "@/lib/provider-tiers";

export interface StakingState {
  /** UI-amount of $GRID currently locked/staked, or null when unknown. */
  stakeAmountUi: number | null;
  /** Whether the provider has opted into a tier above Standard. */
  optedIn: boolean;
  /** Active tier name reported by the backend, if any. */
  tierName: string | null;
}

export function TierLockupCard({ state }: { state: StakingState | null }) {
  const loading = state === null;
  // Standard is the mandatory base lockup — it's the active tier whenever
  // the provider hasn't opted up (or the backend can't tell us yet).
  const activeTier: ProviderTier = state?.optedIn
    ? tierByName(state.tierName)
    : tierByName("Standard");

  // Honest staked headline: only show a number when the backend actually
  // returned one (> 0). The Phase-0 stub returns 0 — we render an
  // explicit empty note instead of "0 $GRID staked" which reads like a
  // real position.
  const hasRealStake =
    typeof state?.stakeAmountUi === "number" && (state?.stakeAmountUi ?? 0) > 0;

  return (
    <Card data-testid="tier-lockup-card">
      <CardHeader>
        <CardTitle>Earnings lockup &amp; tier</CardTitle>
        <p className="mt-1 text-sm text-muted-foreground">
          Every $GRID payout is auto-locked at receipt under the vesting
          program. Your tier sets the cliff, the linear-vest window, and
          your rewards multiplier.
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Stat
            label="Staked / locked $GRID"
            value={
              loading
                ? "…"
                : hasRealStake
                  ? `${state!.stakeAmountUi!.toLocaleString()} $GRID`
                  : "—"
            }
            hint={
              loading || hasRealStake
                ? undefined
                : "No live vesting positions yet"
            }
          />
          <Stat
            label="Current tier"
            value={loading ? "…" : activeTier.name}
            hint={
              loading
                ? undefined
                : state?.optedIn
                  ? "Opted-in"
                  : "Mandatory base lockup"
            }
          />
          <Stat
            label="Rewards multiplier"
            value={loading ? "…" : `${activeTier.multiplier.toFixed(2)}×`}
            hint={loading ? undefined : "Applied to effective work weight"}
          />
        </div>

        <div>
          <p className="mb-2 text-sm font-medium text-foreground">
            Tier ladder
          </p>
          <div className="overflow-hidden rounded-md border border-border">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs uppercase text-muted-foreground">
                <tr>
                  <th className="px-3 py-2 font-medium">Tier</th>
                  <th className="px-3 py-2 font-medium">Cliff</th>
                  <th className="px-3 py-2 font-medium">Linear vest</th>
                  <th className="px-3 py-2 text-right font-medium">
                    Multiplier
                  </th>
                </tr>
              </thead>
              <tbody>
                {PROVIDER_TIERS.map((t) => {
                  const isActive = t.name === activeTier.name;
                  return (
                    <tr
                      key={t.name}
                      className={cn(
                        "border-t border-border",
                        isActive && "bg-success/10 dark:bg-success/15",
                      )}
                    >
                      <td className="px-3 py-2">
                        <span className="font-medium">{t.name}</span>
                        {isActive ? (
                          <span className="ml-2 rounded-full bg-success/15 px-2 py-0.5 text-[10px] font-semibold uppercase text-success dark:bg-success/15 dark:text-success">
                            Active
                          </span>
                        ) : null}
                        <span className="mt-0.5 block text-xs text-muted-foreground">
                          {t.blurb}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">
                        {t.cliffDays}d
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">
                        {t.vestDays}d
                      </td>
                      <td className="px-3 py-2 text-right font-medium">
                        {t.multiplier.toFixed(2)}×
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <p className="mt-2 text-xs text-muted-foreground">
            Rolling, per-payout: each distribution starts its own clock —
            0% during the cliff, then linear to 100% over the vest window.
            Upgrades ratchet upward only and apply to future payouts.
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function Stat({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div className="rounded-md border border-border bg-card p-3">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-lg font-semibold text-foreground">{value}</p>
      {hint ? (
        <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  );
}
