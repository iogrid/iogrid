/**
 * Solana network + token configuration.
 *
 * Resolves all `NEXT_PUBLIC_*` environment variables in one place so
 * the rest of the app reaches for typed constants instead of poking
 * `process.env` directly. Values are baked in at build time (Next.js
 * inlines NEXT_PUBLIC_* into the client bundle).
 *
 * Defaults are intentionally pre-TGE-safe:
 *   - RPC defaults to the public mainnet-beta endpoint (heavily
 *     rate-limited but functional for read-only balance fetches in dev)
 *   - $GRID mint defaults to a deterministic placeholder so dev builds
 *     compile + render. The placeholder is a real-looking SPL mint that
 *     does NOT exist on-chain; balance lookups against it return 0.
 *   - USDC mint is the canonical mainnet USDC (Circle).
 */

import { PublicKey } from "@solana/web3.js";

export const SOLANA_RPC_URL: string =
  process.env.NEXT_PUBLIC_SOLANA_RPC_URL ?? "https://api.mainnet-beta.solana.com";

/**
 * Pre-TGE dummy mint. Replace via `NEXT_PUBLIC_GRID_MINT_ADDRESS` once
 * the token is deployed. The dummy is a known-invalid SPL pubkey —
 * `getTokenAccountsByOwner` will return an empty list for it which
 * keeps the UI happy without throwing.
 */
const DEFAULT_GRID_MINT_PLACEHOLDER =
  "GR1Dxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx";

export const GRID_MINT_ADDRESS: string =
  process.env.NEXT_PUBLIC_GRID_MINT_ADDRESS ?? DEFAULT_GRID_MINT_PLACEHOLDER;

/** Mainnet USDC (Circle issuer). */
export const USDC_MINT_ADDRESS: string =
  "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v";

/**
 * The Solana well-known burn address (incinerator). All buyback-and-burn
 * transfers terminate here; the on-chain log is queried by the public
 * burn dashboard.
 */
export const SOLANA_INCINERATOR_ADDRESS: string =
  "1nc1nerator11111111111111111111111111111111";

/**
 * Parse a base58 pubkey string into a PublicKey. Returns `null` on the
 * placeholder dummy so callers can short-circuit before TGE.
 */
export function safePublicKey(address: string): PublicKey | null {
  if (!address || address === DEFAULT_GRID_MINT_PLACEHOLDER) return null;
  try {
    return new PublicKey(address);
  } catch {
    return null;
  }
}

export function isGridMintConfigured(): boolean {
  return GRID_MINT_ADDRESS !== DEFAULT_GRID_MINT_PLACEHOLDER;
}
