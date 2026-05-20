"use client";

/**
 * /account/wallets — client island that:
 *   - lists bound wallets (GET /api/v1/account/wallets)
 *   - exposes the bind handshake via <WalletBindFlow/>
 *   - allows per-wallet unbind
 *
 * Issue #326 repointed every wallet RPC from the Phase 0 stub at
 * /api/v1/identity/wallets onto the real /api/v1/account/wallets
 * surface that proxies to identity-svc's SIWS Connect-RPC methods.
 *
 * Errors surface inline + via toast so the user gets both a persistent
 * indicator and a transient notification.
 */

import * as React from "react";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { WalletBalance } from "@/components/wallet/WalletBalance";
import { WalletBindFlow } from "@/components/wallet/WalletBindFlow";
import { WalletConnectButton } from "@/components/wallet/WalletConnectButton";
import { WalletAddress } from "@/components/wallet/WalletAddress";
import { browserApi } from "@/lib/api";
import {
  listBoundWallets,
  unbindWallet,
  type BoundWallet,
} from "@/lib/solana/siws";
import { formatRelativeTime } from "@/lib/format";

export function WalletsView() {
  const [wallets, setWallets] = React.useState<BoundWallet[] | null>(null);
  const [loadError, setLoadError] = React.useState<string | null>(null);

  const refresh = React.useCallback(async () => {
    try {
      const res = await listBoundWallets(browserApi());
      setWallets(res.wallets ?? []);
      setLoadError(null);
    } catch (e) {
      setLoadError((e as Error).message);
    }
  }, []);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  const onUnbind = React.useCallback(
    async (address: string) => {
      try {
        await unbindWallet(browserApi(), address);
        toast.success("Wallet unbound.");
        await refresh();
      } catch (e) {
        toast.error((e as Error).message || "Unbind failed");
      }
    },
    [refresh],
  );

  return (
    <div className="space-y-6">
      <WalletBalance />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between space-y-0">
          <div>
            <CardTitle>Bound wallets</CardTitle>
            <p className="mt-1 text-sm text-zinc-500">
              Connect your wallet, sign a one-time challenge, and the
              address is linked to this iogrid identity.
            </p>
          </div>
          <WalletConnectButton size="sm" />
        </CardHeader>
        <CardContent>
          {loadError ? (
            <p className="mb-3 text-sm text-rose-600" data-testid="wallets-load-error">
              Couldn&apos;t load wallets: {loadError}
            </p>
          ) : null}

          {wallets === null ? (
            <p className="text-sm text-zinc-500">Loading…</p>
          ) : wallets.length === 0 ? (
            <p
              className="rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700"
              data-testid="wallets-empty"
            >
              No wallets bound yet. Connect one above, then sign the
              challenge to link it.
            </p>
          ) : (
            <ul className="space-y-2" data-testid="wallets-list">
              {wallets.map((w) => (
                <li
                  key={w.walletAddress}
                  className="flex items-center justify-between gap-3 rounded-md border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900"
                >
                  <div className="flex flex-col gap-1">
                    <WalletAddress address={w.walletAddress} />
                    <p className="text-xs text-zinc-500">
                      Bound {formatRelativeTime(w.boundAt)}
                      {w.label ? ` · ${w.label}` : ""}
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => onUnbind(w.walletAddress)}
                    aria-label={`Unbind wallet ${w.walletAddress}`}
                    data-testid="wallet-unbind-button"
                  >
                    <Trash2 className="h-4 w-4" aria-hidden />
                    <span className="ml-1">Unbind</span>
                  </Button>
                </li>
              ))}
            </ul>
          )}

          <div className="mt-4 border-t border-zinc-200 pt-4 dark:border-zinc-800">
            <p className="mb-2 text-sm font-medium">Add a wallet</p>
            <WalletBindFlow onBound={() => void refresh()} />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
