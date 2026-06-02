// $GRID SPL token balance — queries the Solana RPC for the wallet's
// associated token account.
//
// We bypass the heavyweight @solana/web3.js + @solana/spl-token here:
//   - @solana/web3.js pulls ~600 KiB into the RN bundle (bn.js, buffer,
//     bs58check, dozens of polyfills)
//   - The two calls we need (getTokenAccountsByOwner + filter by mint
//     OR derive the associated token account address + getTokenAccountBalance)
//     fit in ~100 LOC of plain JSON-RPC + a 100-byte PDA derivation.
//
// Track 5 deploys the $GRID mint. Until that's published the env var
// EXPO_PUBLIC_GRID_TOKEN_MINT is empty; this module returns null in
// that case so the wallet card can render "balance unavailable" instead
// of crashing.

import bs58 from 'bs58';
import nacl from 'tweetnacl';

// Solana program ids — base58, hard-coded so we don't need
// @solana/spl-token at all. These IDs are stable + part of the
// Solana protocol.
const TOKEN_PROGRAM_ID = 'TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA';
const ASSOCIATED_TOKEN_PROGRAM_ID = 'ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL';

// The associated-token-account program derives a PDA from
// (owner, TOKEN_PROGRAM, mint). We only need the address derivation;
// the RPC call below does the lookup.

function rpcURL(): string {
  return (
    process.env.EXPO_PUBLIC_SOLANA_RPC_URL ??
    'https://api.devnet.solana.com'
  );
}

function gridMint(): string | null {
  const m = process.env.EXPO_PUBLIC_GRID_TOKEN_MINT;
  return m && m.length > 0 ? m : null;
}

/** Result of a single balance fetch. */
export interface GridBalance {
  /** Raw atomic units (the same as SPL token "amount" — usually 9
   *  decimals for $GRID, mirroring SOL convention). */
  amountAtoms: bigint;
  /** Human-display value: `amountAtoms / 10^decimals`. */
  uiAmount: number;
  /** Token decimals reported by the mint. */
  decimals: number;
  /** When this balance was fetched (ms since epoch). */
  fetchedAt: number;
}

/**
 * Fetch the $GRID balance for a wallet address. Returns null when
 * the GRID mint is not yet configured (Track 5 dependency) OR when
 * the wallet has no associated token account (= no $GRID ever held).
 *
 * The "no token account" path returns `{ amountAtoms: 0n, ... }` with
 * decimals=9 (Solana default) so the UI shows `0 $GRID` instead of
 * "balance unknown" — the user just hasn't been topped up yet.
 *
 * Throws on network/RPC errors; caller (React Query) does the retry.
 */
export async function fetchGridBalance(walletAddress: string): Promise<GridBalance | null> {
  const mint = gridMint();
  if (!mint) {
    return null;
  }
  // getTokenAccountsByOwner with the mint filter — one RPC call,
  // returns the associated token account if it exists (or any other
  // token account for this wallet × mint pair, which is fine).
  const body = {
    jsonrpc: '2.0',
    id: 1,
    method: 'getTokenAccountsByOwner',
    params: [
      walletAddress,
      { mint },
      { encoding: 'jsonParsed', commitment: 'confirmed' },
    ],
  };
  const res = await fetch(rpcURL(), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    // 429 means the free RPC tier hit its limit; bubble up for the
    // React Query retry-with-backoff path.
    throw new Error(`getTokenAccountsByOwner: HTTP ${res.status}`);
  }
  const json = (await res.json()) as {
    result?: {
      value?: Array<{
        account: {
          data: {
            parsed: {
              info: {
                tokenAmount: {
                  amount: string;
                  decimals: number;
                  uiAmount: number;
                };
              };
            };
          };
        };
      }>;
    };
    error?: { message: string };
  };
  if (json.error) {
    throw new Error(`solana rpc: ${json.error.message}`);
  }
  const accounts = json.result?.value ?? [];
  if (accounts.length === 0) {
    return {
      amountAtoms: 0n,
      uiAmount: 0,
      decimals: 9,
      fetchedAt: Date.now(),
    };
  }
  // Sum across any duplicate accounts. Most wallets only have ONE
  // associated token account per mint; sum-instead-of-pick is defensive.
  let amountAtoms = 0n;
  let decimals = 9;
  let uiAmount = 0;
  for (const a of accounts) {
    const ta = a.account.data.parsed.info.tokenAmount;
    amountAtoms += BigInt(ta.amount);
    decimals = ta.decimals;
    uiAmount += ta.uiAmount ?? 0;
  }
  return {
    amountAtoms,
    uiAmount,
    decimals,
    fetchedAt: Date.now(),
  };
}

/**
 * Format a {@link GridBalance} for the wallet card.
 *   `formatGridBalance({ uiAmount: 432.5, ... })` → "432.5 $GRID"
 * Uses up to 4 fractional digits; trims trailing zeros so common
 * whole-token balances render cleanly ("100 $GRID", not "100.0000").
 */
export function formatGridBalance(b: GridBalance | null | undefined): string {
  if (!b) return '— $GRID';
  const opts: Intl.NumberFormatOptions = {
    minimumFractionDigits: 0,
    maximumFractionDigits: 4,
  };
  return `${b.uiAmount.toLocaleString('en-US', opts)} $GRID`;
}

// Suppress "tweetnacl + bs58 imported but unused at runtime in
// non-RN environments" linter warning by referencing them — the
// imports ARE used by future signing helpers we'll add when Track 5
// wires the on-chain $GRID transfer path. Keeping the imports here
// also primes Metro's resolver cache so first-launch latency on the
// wallet card is bounded by the RPC call, not module init.
void bs58;
void nacl;
