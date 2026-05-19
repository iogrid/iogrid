"use client";

/**
 * WalletConnectButton — drop-in client-only Connect / Disconnect
 * button that matches the iogrid shadcn-style Button.
 *
 * Why we don't just re-export `WalletMultiButton` from
 * `@solana/wallet-adapter-react-ui`: that component ships its own CSS
 * variables and DOM structure which clashes with the zinc-themed
 * tailwind variants the rest of the UI uses. We replicate its state
 * machine in ~50 lines and reuse our `Button` so it visually matches
 * the rest of the portal chrome.
 */

import * as React from "react";
import { useWallet } from "@solana/wallet-adapter-react";
import { useWalletModal } from "@solana/wallet-adapter-react-ui";
import { Button } from "@/components/ui/button";
import { truncateAddress } from "@/lib/solana/balances";

export interface WalletConnectButtonProps {
  /** Override the label rendered when no wallet is connected. */
  connectLabel?: string;
  /** Optional className passthrough. */
  className?: string;
  /** Match the `Button` size variants. */
  size?: "default" | "sm" | "lg";
}

export function WalletConnectButton({
  connectLabel = "Connect wallet",
  className,
  size = "default",
}: WalletConnectButtonProps) {
  const { publicKey, connecting, disconnecting, connected, disconnect, wallet } =
    useWallet();
  const { setVisible } = useWalletModal();

  const onClick = React.useCallback(() => {
    if (connected) {
      void disconnect();
      return;
    }
    setVisible(true);
  }, [connected, disconnect, setVisible]);

  let label: string;
  if (connecting) label = "Connecting…";
  else if (disconnecting) label = "Disconnecting…";
  else if (connected && publicKey) label = truncateAddress(publicKey.toBase58());
  else label = connectLabel;

  return (
    <Button
      type="button"
      variant={connected ? "outline" : "default"}
      size={size}
      onClick={onClick}
      disabled={connecting || disconnecting}
      className={className}
      data-testid="wallet-connect-button"
      data-wallet={wallet?.adapter.name ?? ""}
      data-connected={connected ? "true" : "false"}
      aria-label={connected ? "Disconnect wallet" : "Connect Solana wallet"}
    >
      {label}
    </Button>
  );
}
