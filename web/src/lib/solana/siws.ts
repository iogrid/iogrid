/**
 * SIWS (Sign-In-With-Solana) bind-flow helpers.
 *
 * Issue #326 repointed these from the Phase 0 stub at
 * `/api/v1/identity/wallets` (which 200'd an empty list and 501'd on
 * the mutating verbs) onto the real `/api/v1/account/wallets` surface
 * that proxies to identity-svc's SIWS Connect-RPC methods.
 *
 * The gateway-bff exposes endpoints implemented by identity-svc:
 *
 *   POST /api/v1/account/wallets/challenge
 *     body: { walletAddress }
 *     returns: {
 *       nonce: string,         // hex-encoded, single-use (GETDEL on Redis)
 *       challenge: string,     // canonical SIWS message bytes the wallet signs
 *       expiresAt: string,     // ISO-8601, 5 min window
 *     }
 *
 *   POST /api/v1/account/wallets
 *     body: {
 *       walletAddress,
 *       nonce,
 *       signature,             // base58-encoded ed25519 sig over `challenge`
 *     }
 *     returns: BoundWallet
 *
 *   GET  /api/v1/account/wallets
 *     returns: { wallets: BoundWallet[] }
 *
 *   DELETE /api/v1/account/wallets/{walletAddress}
 */

import { ApiClient } from "@/lib/api";

export interface StartBindingResponse {
  nonce: string;
  /** Human-readable challenge text the user signs (RFC-style SIWS payload). */
  challenge: string;
  /** ISO-8601 expiry. */
  expiresAt: string;
}

export interface BoundWallet {
  walletAddress: string;
  chain: "solana";
  boundAt: string;
  /** Display label set by the user (optional). */
  label?: string;
}

export interface ListBoundWalletsResponse {
  wallets: BoundWallet[];
}

const PATH_LIST = "/api/v1/account/wallets";
const PATH_CHALLENGE = "/api/v1/account/wallets/challenge";

export async function startSiwsBinding(
  client: ApiClient,
  walletAddress: string,
): Promise<StartBindingResponse> {
  return client.post<StartBindingResponse>(PATH_CHALLENGE, { walletAddress });
}

export async function completeSiwsBinding(
  client: ApiClient,
  args: { walletAddress: string; nonce: string; signature: string },
): Promise<BoundWallet> {
  return client.post<BoundWallet>(PATH_LIST, args);
}

export async function listBoundWallets(
  client: ApiClient,
): Promise<ListBoundWalletsResponse> {
  return client.get<ListBoundWalletsResponse>(PATH_LIST);
}

export async function unbindWallet(
  client: ApiClient,
  walletAddress: string,
): Promise<void> {
  await client.del(`${PATH_LIST}/${encodeURIComponent(walletAddress)}`);
}

/**
 * Encode a wallet-adapter signMessage() result (Uint8Array) into the
 * base58 string the server expects. Inlined to avoid pulling `bs58`
 * as a top-level dep — wallet-adapter signs locally so the bundle
 * impact would be a transitive duplication.
 *
 * Reference: https://datatracker.ietf.org/doc/html/draft-msporny-base58-01
 */
const BASE58_ALPHABET =
  "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";

export function encodeSignature(sig: Uint8Array): string {
  if (sig.length === 0) return "";
  // Count leading zero bytes — each becomes a leading "1".
  let zeroes = 0;
  while (zeroes < sig.length && sig[zeroes] === 0) zeroes += 1;

  // Convert big-endian bytes to base-58 digits.
  const digits: number[] = [];
  for (let i = zeroes; i < sig.length; i += 1) {
    let carry = sig[i];
    for (let j = 0; j < digits.length; j += 1) {
      carry += digits[j] << 8;
      digits[j] = carry % 58;
      carry = Math.floor(carry / 58);
    }
    while (carry > 0) {
      digits.push(carry % 58);
      carry = Math.floor(carry / 58);
    }
  }

  let out = "";
  for (let i = 0; i < zeroes; i += 1) out += BASE58_ALPHABET[0];
  for (let i = digits.length - 1; i >= 0; i -= 1) out += BASE58_ALPHABET[digits[i]];
  return out;
}
