/**
 * Pure helpers + hook for reading SOL / SPL token balances.
 *
 * Kept dep-light: we don't pull `@solana/spl-token` (it adds ~120kB
 * gzipped). Instead we call the JSON-RPC method
 * `getTokenAccountsByOwner` directly via `Connection.getParsedTokenAccountsByOwner`,
 * which is bundled inside `@solana/web3.js` already.
 */

import * as React from "react";
import { Connection, PublicKey, LAMPORTS_PER_SOL } from "@solana/web3.js";
import {
  GRID_MINT_ADDRESS,
  USDC_MINT_ADDRESS,
  isGridMintConfigured,
  safePublicKey,
} from "./config";

export interface TokenBalance {
  /** UI amount as a decimal number, rounded to the mint's decimals. */
  uiAmount: number;
  /** Raw amount as the base-units string (preserves precision). */
  rawAmount: string;
  decimals: number;
}

export interface WalletBalances {
  sol: TokenBalance;
  grid: TokenBalance;
  usdc: TokenBalance;
}

const ZERO: TokenBalance = { uiAmount: 0, rawAmount: "0", decimals: 0 };

/**
 * Fetch SOL + $GRID + USDC for a given owner. Errors are swallowed
 * per-token so a missing $GRID mint pre-TGE doesn't blank the whole
 * widget — that token simply reports zero.
 */
export async function fetchWalletBalances(
  connection: Connection,
  owner: PublicKey,
): Promise<WalletBalances> {
  const solLamports = await connection.getBalance(owner).catch(() => 0);
  const sol: TokenBalance = {
    uiAmount: solLamports / LAMPORTS_PER_SOL,
    rawAmount: String(solLamports),
    decimals: 9,
  };

  const gridMint = isGridMintConfigured() ? safePublicKey(GRID_MINT_ADDRESS) : null;
  const usdcMint = safePublicKey(USDC_MINT_ADDRESS);

  const [grid, usdc] = await Promise.all([
    gridMint ? readSplBalance(connection, owner, gridMint) : Promise.resolve(ZERO),
    usdcMint ? readSplBalance(connection, owner, usdcMint) : Promise.resolve(ZERO),
  ]);

  return { sol, grid, usdc };
}

async function readSplBalance(
  connection: Connection,
  owner: PublicKey,
  mint: PublicKey,
): Promise<TokenBalance> {
  try {
    const res = await connection.getParsedTokenAccountsByOwner(owner, { mint });
    if (res.value.length === 0) return ZERO;
    // Sum across all token accounts (Phantom typically opens one ATA
    // per mint but a user may have manually created secondaries).
    let raw = 0n;
    let decimals = 0;
    for (const acc of res.value) {
      const info = acc.account.data.parsed?.info;
      const ta = info?.tokenAmount;
      if (!ta) continue;
      raw += BigInt(ta.amount ?? "0");
      decimals = Number(ta.decimals ?? decimals);
    }
    const divisor = 10n ** BigInt(decimals || 0);
    const ui = decimals === 0 ? Number(raw) : Number(raw) / Number(divisor);
    return { uiAmount: ui, rawAmount: raw.toString(), decimals };
  } catch {
    return ZERO;
  }
}

/**
 * useWalletBalances — React hook that polls balances every 30 seconds.
 * Returns `null` until the first fetch resolves; safe to render
 * unconditionally because it never throws.
 */
export function useWalletBalances(
  connection: Connection | null,
  owner: PublicKey | null,
  intervalMs = 30_000,
): { balances: WalletBalances | null; loading: boolean; error: string | null } {
  const [balances, setBalances] = React.useState<WalletBalances | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (!connection || !owner) {
      setBalances(null);
      return;
    }
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      try {
        const b = await fetchWalletBalances(connection, owner);
        if (!cancelled) {
          setBalances(b);
          setError(null);
        }
      } catch (e) {
        if (!cancelled) setError((e as Error).message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    void load();
    const id = setInterval(load, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [connection, owner, intervalMs]);

  return { balances, loading, error };
}

/** Truncate a base58 address to the standard "abcd…wxyz" form. */
export function truncateAddress(addr: string, head = 4, tail = 4): string {
  if (!addr) return "";
  if (addr.length <= head + tail + 1) return addr;
  return `${addr.slice(0, head)}…${addr.slice(-tail)}`;
}

/** Format a token UI amount with the right precision. */
export function formatToken(amount: number, maxFractionDigits = 4): string {
  if (!Number.isFinite(amount)) return "—";
  return new Intl.NumberFormat("en-US", {
    maximumFractionDigits: maxFractionDigits,
    minimumFractionDigits: 0,
  }).format(amount);
}
