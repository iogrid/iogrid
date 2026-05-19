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
import { WalletBalance } from "@/components/wallet/WalletBalance";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { StakeForm } from "@/components/wallet/StakeForm";
import { StakePositionsTable } from "@/components/wallet/StakePositionsTable";
import { browserApi } from "@/lib/api";
import {
  claimYield,
  earlyUnlock,
  listStakePositions,
  openStakePosition,
  type LockPeriodDays,
  type StakePosition,
} from "@/lib/solana/staking";
import { fetchWalletBalances } from "@/lib/solana/balances";

export function StakingView() {
  const { connection } = useConnection();
  const { publicKey, connected } = useWallet();

  const [positions, setPositions] = React.useState<StakePosition[] | null>(null);
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
    void refresh();
  }, [refresh]);

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

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <WalletBalance />
        <Card>
          <CardHeader className="flex flex-row items-start justify-between space-y-0">
            <div>
              <CardTitle>Open a new stake</CardTitle>
              <p className="mt-1 text-sm text-zinc-500">
                Locked tokens count toward routing-priority weight for
                the full period.
              </p>
            </div>
            {!connected ? <WalletConnectButton size="sm" /> : null}
          </CardHeader>
          <CardContent>
            {!connected ? (
              <p
                className="rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700"
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
        <p className="text-sm text-rose-600" data-testid="staking-load-error">
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
