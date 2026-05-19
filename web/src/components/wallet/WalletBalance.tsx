"use client";

/**
 * WalletBalance — three-row balance card for the connected wallet.
 * Polls SOL + $GRID + USDC every 30s. Renders an inline empty-state
 * when no wallet is connected; renders a loader on first fetch.
 *
 * Visually matches the `StatsCard` pattern but groups three tokens in
 * a single bordered tile so the wallet pages don't feel sparse.
 */

import * as React from "react";
import { useConnection, useWallet } from "@solana/wallet-adapter-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  formatToken,
  useWalletBalances,
  type TokenBalance,
} from "@/lib/solana/balances";
import { isGridMintConfigured } from "@/lib/solana/config";
import { WalletAddress } from "./WalletAddress";

export interface WalletBalanceProps {
  /** Optional title override. */
  title?: string;
  /** Hide the wallet address pill. */
  hideAddress?: boolean;
  className?: string;
}

export function WalletBalance({
  title = "Wallet balance",
  hideAddress,
  className,
}: WalletBalanceProps) {
  const { connection } = useConnection();
  const { publicKey, connected } = useWallet();
  const { balances, loading, error } = useWalletBalances(
    connected ? connection : null,
    publicKey ?? null,
  );

  return (
    <Card className={className} data-testid="wallet-balance-card">
      <CardHeader className="flex flex-row items-start justify-between space-y-0">
        <CardTitle>{title}</CardTitle>
        {connected && publicKey && !hideAddress ? (
          <WalletAddress address={publicKey.toBase58()} />
        ) : null}
      </CardHeader>
      <CardContent>
        {!connected ? (
          <p className="text-sm text-zinc-500" data-testid="wallet-balance-empty">
            Connect a Solana wallet to view balances.
          </p>
        ) : error ? (
          <p className="text-sm text-rose-600">Couldn&apos;t load balances: {error}</p>
        ) : !balances ? (
          <p className="text-sm text-zinc-500">{loading ? "Loading…" : "—"}</p>
        ) : (
          <ul className="space-y-2" data-testid="wallet-balance-list">
            <BalanceRow
              symbol="$GRID"
              balance={balances.grid}
              hint={isGridMintConfigured() ? undefined : "Pre-TGE — mint not yet configured"}
            />
            <BalanceRow symbol="SOL" balance={balances.sol} />
            <BalanceRow symbol="USDC" balance={balances.usdc} />
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function BalanceRow({
  symbol,
  balance,
  hint,
}: {
  symbol: string;
  balance: TokenBalance;
  hint?: string;
}) {
  return (
    <li
      className="flex items-center justify-between rounded-md border border-zinc-100 bg-zinc-50 px-3 py-2 text-sm dark:border-zinc-800 dark:bg-zinc-900"
      data-symbol={symbol}
    >
      <div>
        <p className="font-medium">{symbol}</p>
        {hint ? <p className="text-xs text-zinc-500">{hint}</p> : null}
      </div>
      <p className="font-mono tabular-nums" data-testid={`balance-${symbol}`}>
        {formatToken(balance.uiAmount, symbol === "SOL" ? 4 : 2)}
      </p>
    </li>
  );
}
