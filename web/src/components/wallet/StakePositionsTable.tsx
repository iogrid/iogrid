"use client";

/**
 * StakePositionsTable — renders the user's active stake positions
 * with a per-row Claim and Early-unlock action.
 *
 * Early-unlock surfaces a confirmation modal warning that 50% of the
 * locked principal will be burned (per docs/TOKENOMICS.md Layer-3).
 */

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { formatToken } from "@/lib/solana/balances";
import { formatRemainingLock, type StakePosition } from "@/lib/solana/staking";

export interface StakePositionsTableProps {
  positions: StakePosition[];
  onClaim: (positionId: string) => Promise<void>;
  onEarlyUnlock: (positionId: string) => Promise<void>;
  loading?: boolean;
}

export function StakePositionsTable({
  positions,
  onClaim,
  onEarlyUnlock,
  loading,
}: StakePositionsTableProps) {
  const [pendingEarlyUnlock, setPendingEarlyUnlock] = React.useState<StakePosition | null>(null);
  const [busyId, setBusyId] = React.useState<string | null>(null);

  const handleClaim = async (id: string) => {
    setBusyId(id);
    try {
      await onClaim(id);
    } finally {
      setBusyId(null);
    }
  };

  const handleConfirmEarlyUnlock = async () => {
    if (!pendingEarlyUnlock) return;
    setBusyId(pendingEarlyUnlock.id);
    try {
      await onEarlyUnlock(pendingEarlyUnlock.id);
      setPendingEarlyUnlock(null);
    } finally {
      setBusyId(null);
    }
  };

  return (
    <Card data-testid="stake-positions-table">
      <CardHeader>
        <CardTitle>Active stake positions</CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : positions.length === 0 ? (
          <p
            className="rounded-md border border-dashed border-border-strong p-4 text-center text-sm text-muted-foreground dark:border-border-strong"
            data-testid="stake-positions-empty"
          >
            No active stake positions. Use the form above to open one.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs uppercase tracking-wide text-muted-foreground dark:border-border">
                  <th className="py-2 pr-3 font-medium">Position</th>
                  <th className="py-2 pr-3 font-medium">Amount</th>
                  <th className="py-2 pr-3 font-medium">Tier</th>
                  <th className="py-2 pr-3 font-medium">Remaining</th>
                  <th className="py-2 pr-3 font-medium">Accrued</th>
                  <th className="py-2 pr-3 font-medium" />
                </tr>
              </thead>
              <tbody>
                {positions.map((p) => (
                  <tr
                    key={p.id}
                    className="border-b border-border last:border-0 dark:border-border"
                    data-testid={`stake-position-${p.id}`}
                  >
                    <td className="py-2 pr-3 font-mono text-xs text-muted-foreground">
                      {p.id.slice(0, 8)}…
                    </td>
                    <td className="py-2 pr-3 font-mono tabular-nums">
                      {formatToken(p.amountUi)} $GRID
                    </td>
                    <td className="py-2 pr-3">
                      <span className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs dark:bg-muted">
                        {p.lockPeriodDays}d · {p.tierMultiplier.toFixed(2)}×
                      </span>
                    </td>
                    <td
                      className={cn(
                        "py-2 pr-3 font-mono tabular-nums",
                        p.unlocked ? "text-success" : "",
                      )}
                    >
                      {formatRemainingLock(p.unlocksAt)}
                    </td>
                    <td className="py-2 pr-3 font-mono tabular-nums">
                      {formatToken(p.accruedYieldUi)}
                    </td>
                    <td className="flex gap-2 py-2 pr-3">
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={busyId === p.id || p.accruedYieldUi <= 0}
                        onClick={() => handleClaim(p.id)}
                        data-testid={`stake-claim-${p.id}`}
                      >
                        Claim
                      </Button>
                      {!p.unlocked ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          disabled={busyId === p.id}
                          onClick={() => setPendingEarlyUnlock(p)}
                          data-testid={`stake-early-unlock-${p.id}`}
                          className="text-destructive"
                        >
                          Early unlock
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>

      {pendingEarlyUnlock ? (
        <EarlyUnlockModal
          position={pendingEarlyUnlock}
          onCancel={() => setPendingEarlyUnlock(null)}
          onConfirm={handleConfirmEarlyUnlock}
          confirming={busyId === pendingEarlyUnlock.id}
        />
      ) : null}
    </Card>
  );
}

function EarlyUnlockModal({
  position,
  onCancel,
  onConfirm,
  confirming,
}: {
  position: StakePosition;
  onCancel: () => void;
  onConfirm: () => void;
  confirming: boolean;
}) {
  const burned = position.amountUi * 0.5;
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="early-unlock-title"
      data-testid="early-unlock-modal"
      className="fixed inset-0 z-50 flex items-center justify-center bg-foreground/10 p-4 backdrop-blur-sm"
    >
      <div className="w-full max-w-md rounded-lg border border-border bg-card p-6 shadow-xl dark:border-border">
        <h2
          id="early-unlock-title"
          className="text-lg font-semibold text-destructive"
        >
          Early unlock — 50% burn warning
        </h2>
        <p className="mt-3 text-sm text-muted-foreground dark:text-muted-foreground">
          You&apos;re ending this stake position early. Half of the
          principal — <strong>{formatToken(burned)} $GRID</strong> — will
          be permanently burned to the Solana incinerator address. The
          other half returns to your wallet immediately.
        </p>
        <p className="mt-3 text-xs text-muted-foreground">
          You can perform an early-unlock at most once per 12 months per
          provider. See docs/TOKENOMICS.md §Layer-3 for the full rules.
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            type="button"
            onClick={onConfirm}
            disabled={confirming}
            data-testid="early-unlock-confirm"
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {confirming ? "Burning…" : "Burn 50% and unlock"}
          </Button>
        </div>
      </div>
    </div>
  );
}
