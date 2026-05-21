"use client";

/**
 * WalletBindFlow — orchestrates the SIWS bind handshake:
 *   1. user clicks "Add wallet" → opens wallet-adapter modal
 *   2. once connected → POST start-binding, receive challenge + nonce
 *   3. ask wallet to signMessage(challenge)
 *   4. POST complete-binding with the signature
 *   5. refresh list via the supplied callback
 *
 * Exported as a controlled component so the parent page can wrap it
 * with React Query / SWR. The component itself is dep-free beyond the
 * wallet-adapter hooks + our ApiClient.
 */

import * as React from "react";
import { useWallet } from "@solana/wallet-adapter-react";
import { useWalletModal } from "@solana/wallet-adapter-react-ui";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import {
  completeSiwsBinding,
  encodeSignature,
  startSiwsBinding,
  type BoundWallet,
} from "@/lib/solana/siws";

export interface WalletBindFlowProps {
  /** Called when a new wallet is bound. */
  onBound?: (wallet: BoundWallet) => void;
  /**
   * Optional dependency injection — primarily for unit tests so they
   * can provide an `ApiClient` backed by a `fetcher` mock. Production
   * callers leave undefined and the component reaches for
   * `browserApi()`.
   */
  apiClient?: ReturnType<typeof browserApi>;
  className?: string;
}

type Status =
  | { kind: "idle" }
  | { kind: "starting" }
  | { kind: "awaiting-signature"; challenge: string; nonce: string }
  | { kind: "completing" }
  | { kind: "error"; message: string };

export function WalletBindFlow({ onBound, apiClient, className }: WalletBindFlowProps) {
  const wallet = useWallet();
  const { setVisible } = useWalletModal();
  const [status, setStatus] = React.useState<Status>({ kind: "idle" });

  const begin = React.useCallback(async () => {
    if (!wallet.connected || !wallet.publicKey) {
      // Not connected yet — open the wallet-adapter modal and bail.
      setVisible(true);
      return;
    }
    if (!wallet.signMessage) {
      toast.error("This wallet doesn't support message signing.");
      return;
    }
    const client = apiClient ?? browserApi();
    const address = wallet.publicKey.toBase58();
    try {
      setStatus({ kind: "starting" });
      const { challenge, nonce } = await startSiwsBinding(client, address);
      setStatus({ kind: "awaiting-signature", challenge, nonce });

      const encoder = new TextEncoder();
      const sigBytes = await wallet.signMessage(encoder.encode(challenge));
      setStatus({ kind: "completing" });

      const bound = await completeSiwsBinding(client, {
        walletAddress: address,
        nonce,
        signature: encodeSignature(sigBytes),
      });
      toast.success("Wallet bound to your account.");
      onBound?.(bound);
      setStatus({ kind: "idle" });
    } catch (e) {
      const message = (e as Error).message || "Binding failed";
      setStatus({ kind: "error", message });
      toast.error(message);
    }
  }, [wallet, setVisible, apiClient, onBound]);

  const busy =
    status.kind === "starting" ||
    status.kind === "awaiting-signature" ||
    status.kind === "completing";

  let label = wallet.connected ? "Sign & bind wallet" : "Connect & bind wallet";
  if (status.kind === "starting") label = "Requesting challenge…";
  else if (status.kind === "awaiting-signature") label = "Sign the challenge in your wallet…";
  else if (status.kind === "completing") label = "Verifying signature…";

  return (
    <div className={className}>
      <Button
        type="button"
        onClick={begin}
        disabled={busy}
        data-testid="wallet-bind-button"
        data-status={status.kind}
      >
        {label}
      </Button>
      {status.kind === "error" ? (
        <p className="mt-2 text-sm text-destructive" data-testid="wallet-bind-error">
          {status.message}
        </p>
      ) : null}
    </div>
  );
}
