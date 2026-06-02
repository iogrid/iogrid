"use client";

/**
 * Provider staking page — orchestrates the StakeForm + positions table
 * around the gateway-bff endpoints. Falls back gracefully when the
 * user hasn't connected a wallet yet (the gateway only returns
 * positions for the calling identity, but the UX nudges the user to
 * bind first so they understand WHERE the yield will land).
 */

import * as React from "react";
import Link from "next/link";
import { useConnection, useWallet } from "@solana/wallet-adapter-react";
import { toast } from "sonner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ProviderEmptyState,
  PROVIDER_EMPTY_STAKING_SUBTITLE,
} from "@/components/dashboard/provider-empty-state";
import { WalletBalance } from "@/components/wallet/WalletBalance";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { StakeForm } from "@/components/wallet/StakeForm";
import { StakePositionsTable } from "@/components/wallet/StakePositionsTable";
import { browserApi } from "@/lib/api";
import { useProviderOwnership } from "@/lib/use-provider-ownership";
import {
  claimYield,
  earlyUnlock,
  listStakePositions,
  openStakePosition,
  type LockPeriodDays,
  type StakePosition,
} from "@/lib/solana/staking";
import { fetchWalletBalances } from "@/lib/solana/balances";
import { TierLockupCard, type StakingState } from "./tier-lockup-card";
import { PingWalletCard } from "./ping-wallet-card";

/** Raw GET /api/v1/staking/ envelope (gateway-bff emptyStakingState). */
interface RawStakingState {
  stake_amount?: number;
  stakeAmount?: number;
  opted_in?: boolean;
  optedIn?: boolean;
  tier?: string | null;
  tier_name?: string | null;
}

export function StakingView() {
  const ownership = useProviderOwnership();
  const { connection } = useConnection();
  const { publicKey, connected } = useWallet();

  const [positions, setPositions] = React.useState<StakePosition[] | null>(null);
  const [stakingState, setStakingState] = React.useState<StakingState | null>(
    null,
  );
  const [availableGrid, setAvailableGrid] = React.useState<number>(0);
  const [loadError, setLoadError] = React.useState<string | null>(null);

  const refresh = React.useCallback(async () => {
    try {
      const res = await listStakePositions(browserApi());
      setPositions(res.positions ?? []);
      setLoadError(null);
    } catch (e) {
      setLoadError((e as Error).message);
    }
    // Real opt-in / tier state. The backend is currently the Phase-0
    // empty stub (opted_in:false, stake_amount:0); we map that to an
    // honest "Standard base lockup, no live positions" state rather
    // than fabricating a staked balance (#634 / #417).
    try {
      const raw = await browserApi().get<RawStakingState>("/api/v1/staking/");
      const amount = raw.stake_amount ?? raw.stakeAmount ?? null;
      setStakingState({
        stakeAmountUi: typeof amount === "number" ? amount : null,
        optedIn: Boolean(raw.opted_in ?? raw.optedIn ?? false),
        tierName: raw.tier_name ?? raw.tier ?? null,
      });
    } catch {
      // Leave stakingState null → TierLockupCard renders its loading
      // shimmer; it never invents numbers.
    }
    if (connected && publicKey) {
      try {
        const b = await fetchWalletBalances(connection, publicKey);
        setAvailableGrid(b.grid.uiAmount);
      } catch {
        // Silent — balance widget surfaces its own error.
      }
    } else {
      setAvailableGrid(0);
    }
  }, [connected, publicKey, connection]);

  React.useEffect(() => {
    // Suppress the positions probe when ownership says no — saves a
    // round-trip per page view for the not-yet-paired cohort (#313).
    if (ownership.hasProvider === false) return;
    void refresh();
  }, [refresh, ownership.hasProvider]);

  const handleStake = React.useCallback(
    async (amount: string, lockPeriodDays: LockPeriodDays) => {
      try {
        await openStakePosition(browserApi(), { amount, lockPeriodDays });
        toast.success("Stake position opened.");
        await refresh();
      } catch (e) {
        toast.error((e as Error).message || "Stake failed");
        throw e;
      }
    },
    [refresh],
  );

  const handleClaim = React.useCallback(
    async (positionId: string) => {
      try {
        await claimYield(browserApi(), positionId);
        toast.success("Yield claimed.");
        await refresh();
      } catch (e) {
        toast.error((e as Error).message || "Claim failed");
      }
    },
    [refresh],
  );

  const handleEarlyUnlock = React.useCallback(
    async (positionId: string) => {
      try {
        const { burnedAmountUi } = await earlyUnlock(browserApi(), positionId);
        toast.success(`Position unlocked — ${burnedAmountUi.toFixed(2)} $GRID burned.`);
        await refresh();
      } catch (e) {
        toast.error((e as Error).message || "Early unlock failed");
      }
    },
    [refresh],
  );

  // Gate on ownership BEFORE rendering the wallet + stake form (#313).
  // Staking requires routing yield to a paired provider — without one,
  // there's no provider to weight, so we point the operator at /install.
  if (ownership.hasProvider === false) {
    return <ProviderEmptyState subtitle={PROVIDER_EMPTY_STAKING_SUBTITLE} />;
  }

  return (
    <div className="space-y-6">
      <TierLockupCard state={stakingState} />

      <PingWalletCard />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <WalletBalance />
        <Card>
          <CardHeader className="flex flex-row items-start justify-between space-y-0">
            <div>
              <CardTitle>Open a new stake</CardTitle>
              <p className="mt-1 text-sm text-muted-foreground">
                Locked tokens count toward routing-priority weight for
                the full period.
              </p>
            </div>
            {!connected ? <WalletConnectButton size="sm" /> : null}
          </CardHeader>
          <CardContent>
            {!connected ? (
              <p
                className="rounded-md border border-dashed border-border-strong p-4 text-center text-sm text-muted-foreground dark:border-border-strong"
                data-testid="staking-needs-wallet"
              >
                Connect a wallet to open stake positions. Manage bound
                wallets in{" "}
                <Link href="/account/wallets" className="underline">
                  Account → Wallets
                </Link>
                .
              </p>
            ) : (
              <StakeForm
                availableGrid={availableGrid}
                onSubmit={handleStake}
              />
            )}
          </CardContent>
        </Card>
      </div>

      {loadError ? (
        <p className="text-sm text-destructive" data-testid="staking-load-error">
          Couldn&apos;t load positions: {loadError}
        </p>
      ) : null}

      <StakePositionsTable
        positions={positions ?? []}
        loading={positions === null}
        onClaim={handleClaim}
        onEarlyUnlock={handleEarlyUnlock}
      />
    </div>
  );
}
