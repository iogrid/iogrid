"use client";

/**
 * StakeForm — open a new lock position. Pure controlled component;
 * the parent owns the mutation (so we can drop in React Query
 * mutations + invalidate the positions list).
 */

import * as React from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import {
  STAKING_TIERS,
  tierFor,
  type LockPeriodDays,
} from "@/lib/solana/staking";
import { formatToken } from "@/lib/solana/balances";

export interface StakeFormProps {
  /** Available $GRID balance (UI form). */
  availableGrid: number;
  /** Mutation handler — receives validated amount + lockPeriod. */
  onSubmit: (amount: string, lockPeriodDays: LockPeriodDays) => Promise<void>;
  className?: string;
}

export function StakeForm({ availableGrid, onSubmit, className }: StakeFormProps) {
  const [amount, setAmount] = React.useState("");
  const [period, setPeriod] = React.useState<LockPeriodDays>(30);
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  const tier = tierFor(period);
  const amountNum = Number(amount);
  const exceedsBalance = Number.isFinite(amountNum) && amountNum > availableGrid;
  const previewMultiplied =
    Number.isFinite(amountNum) && amountNum > 0 ? amountNum * tier.multiplier : 0;

  const submit = React.useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setError(null);
      if (!amount || amountNum <= 0) {
        setError("Enter a positive amount.");
        return;
      }
      if (exceedsBalance) {
        setError("Amount exceeds your available $GRID balance.");
        return;
      }
      try {
        setBusy(true);
        await onSubmit(amount, period);
        setAmount("");
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [amount, amountNum, exceedsBalance, onSubmit, period],
  );

  return (
    <form
      className={cn("space-y-4", className)}
      onSubmit={submit}
      data-testid="stake-form"
    >
      <div>
        <label
          htmlFor="stake-amount"
          className="mb-1 block text-sm font-medium"
        >
          Amount ($GRID)
        </label>
        <div className="flex gap-2">
          <Input
            id="stake-amount"
            type="number"
            inputMode="decimal"
            step="any"
            min="0"
            placeholder="0.00"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            data-testid="stake-amount-input"
          />
          <Button
            type="button"
            variant="outline"
            onClick={() => setAmount(String(availableGrid))}
          >
            Max
          </Button>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          Available: {formatToken(availableGrid)} $GRID
        </p>
      </div>

      <div>
        <p className="mb-1 text-sm font-medium">Lock period</p>
        <div
          className="grid grid-cols-2 gap-2 sm:grid-cols-4"
          role="radiogroup"
          aria-label="Stake lock period"
        >
          {STAKING_TIERS.map((t) => (
            <button
              key={t.lockPeriodDays}
              type="button"
              role="radio"
              aria-checked={period === t.lockPeriodDays}
              onClick={() => setPeriod(t.lockPeriodDays)}
              data-testid={`stake-period-${t.lockPeriodDays}`}
              className={cn(
                "rounded-md border p-3 text-left text-sm transition-colors",
                period === t.lockPeriodDays
                  ? "border-foreground bg-foreground text-background dark:border-foreground dark:bg-foreground dark:text-background"
                  : "border-border hover:border-foreground/40 dark:border-border",
              )}
            >
              <p className="font-medium">{t.lockPeriodDays}d</p>
              <p className="text-xs opacity-75">{t.name}</p>
              <p className="text-xs opacity-75">{t.multiplier.toFixed(2)}×</p>
            </button>
          ))}
        </div>
      </div>

      <div className="rounded-md border border-border bg-muted p-3 text-sm dark:border-border dark:bg-card">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground dark:text-muted-foreground">
            Tier multiplier
          </span>
          <span className="font-mono font-semibold">
            {tier.multiplier.toFixed(2)}×
          </span>
        </div>
        <div className="mt-1 flex items-center justify-between">
          <span className="text-muted-foreground dark:text-muted-foreground">
            Stake weight preview
          </span>
          <span
            className="font-mono font-semibold"
            data-testid="stake-preview"
          >
            {formatToken(previewMultiplied)} $GRID
          </span>
        </div>
      </div>

      {error ? (
        <p className="text-sm text-destructive" data-testid="stake-form-error">
          {error}
        </p>
      ) : null}

      <Button
        type="submit"
        disabled={busy || !amount || exceedsBalance}
        data-testid="stake-submit-button"
      >
        {busy ? "Staking…" : "Open stake position"}
      </Button>
    </form>
  );
}
