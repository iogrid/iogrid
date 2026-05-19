"use client";

/**
 * WalletAddress — renders a base58 address as a copyable, truncated
 * pill. Click anywhere on the pill copies the full address to
 * clipboard and surfaces a Sonner toast.
 *
 * Pure visual component; takes the address as a prop so it works for
 * BOTH the currently-connected wallet and any wallet from a
 * server-rendered list (e.g. /account/wallets bind history).
 */

import * as React from "react";
import { toast } from "sonner";
import { Copy, Check } from "lucide-react";
import { truncateAddress } from "@/lib/solana/balances";
import { cn } from "@/lib/utils";

export interface WalletAddressProps {
  address: string;
  className?: string;
  /** When `false`, render the full address without truncation. */
  truncate?: boolean;
  /** Head + tail char counts for the truncated form. */
  head?: number;
  tail?: number;
}

export function WalletAddress({
  address,
  className,
  truncate = true,
  head = 4,
  tail = 4,
}: WalletAddressProps) {
  const [copied, setCopied] = React.useState(false);
  const display = truncate ? truncateAddress(address, head, tail) : address;

  const onCopy = React.useCallback(async () => {
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
      toast.success("Address copied", { duration: 1500 });
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Couldn't access clipboard");
    }
  }, [address]);

  return (
    <button
      type="button"
      onClick={onCopy}
      title={address}
      aria-label={`Copy wallet address ${address}`}
      data-testid="wallet-address"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border border-zinc-200 bg-zinc-50 px-2 py-1 font-mono text-xs text-zinc-700 transition-colors hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800",
        className,
      )}
    >
      <span>{display}</span>
      {copied ? (
        <Check className="h-3.5 w-3.5 text-emerald-600" aria-hidden />
      ) : (
        <Copy className="h-3.5 w-3.5 text-zinc-400" aria-hidden />
      )}
    </button>
  );
}
