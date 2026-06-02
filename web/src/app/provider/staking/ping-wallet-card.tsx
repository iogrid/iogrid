"use client";

/**
 * PingWalletCard — surfaces the provider's Ping-wallet relationship on
 * the staking page (issue #634). $GRID payouts off-ramp to USDC through
 * the provider's Ping wallet (auto-swap on receipt via Jupiter, ADR
 * 0007 — see docs/TOKENOMICS.md "Ping integration pointer").
 *
 * Data is REAL where it exists: the linked wallet is the Solana wallet
 * the provider bound via SIWS at GET /api/v1/account/wallets (the same
 * key Ping's auto-swap settles to). When no wallet is bound we render a
 * "Connect Ping wallet" CTA pointing at /account/wallets rather than
 * fabricating an address (#417 anti-fake guardrail).
 *
 * NOTE: iogrid binds a generic Solana wallet; "Ping wallet" is the
 * product framing because that bound key is what Ping's off-ramp swaps
 * into USDC. There is no separate Ping-specific binding endpoint yet —
 * the $GRID mint itself is pre-launch (#629), so we surface the
 * relationship + destination, not a live swap balance.
 */

import * as React from "react";
import Link from "next/link";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { WalletAddress } from "@/components/wallet/WalletAddress";
import { browserApi } from "@/lib/api";

interface BoundWallet {
  walletAddress: string;
  chain: string;
  boundAt: string;
}

interface WalletsResponse {
  wallets: BoundWallet[];
}

export function PingWalletCard() {
  const [wallets, setWallets] = React.useState<BoundWallet[] | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    browserApi()
      .get<WalletsResponse>("/api/v1/account/wallets")
      .then((res) => {
        if (!cancelled) setWallets(res.wallets ?? []);
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const linked = wallets && wallets.length > 0 ? wallets[0] : null;
  const loading = wallets === null && error === null;

  return (
    <Card data-testid="ping-wallet-card">
      <CardHeader>
        <CardTitle>Ping off-ramp wallet</CardTitle>
        <p className="mt-1 text-sm text-muted-foreground">
          Locked $GRID vests to this wallet. Ping auto-swaps it to USDC on
          receipt so you can cash out without touching a DEX.
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <p className="text-sm text-muted-foreground">Loading wallet…</p>
        ) : error ? (
          <p className="text-sm text-destructive" data-testid="ping-wallet-error">
            Couldn&apos;t load wallet: {error}
          </p>
        ) : linked ? (
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm text-muted-foreground">Linked wallet</span>
              <WalletAddress address={linked.walletAddress} />
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm text-muted-foreground">Settlement</span>
              <span className="text-sm font-medium text-foreground">
                $GRID → USDC (auto-swap)
              </span>
            </div>
            <p className="text-xs text-muted-foreground">
              Manage bound wallets in{" "}
              <Link href="/account/wallets" className="underline">
                Account → Wallets
              </Link>
              . The $GRID mint is pre-launch — swaps activate at TGE.
            </p>
          </div>
        ) : (
          <div
            className="rounded-md border border-dashed border-border-strong p-4 text-center dark:border-border-strong"
            data-testid="ping-wallet-cta"
          >
            <p className="text-sm text-muted-foreground">
              No Ping wallet linked yet. Bind a Solana wallet to receive
              your vested $GRID and auto-swap it to USDC.
            </p>
            <Link
              href="/account/wallets"
              className="mt-3 inline-flex items-center gap-1 rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background hover:bg-foreground/80 dark:bg-foreground dark:text-background"
            >
              Connect Ping wallet
              <span aria-hidden>→</span>
            </Link>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
