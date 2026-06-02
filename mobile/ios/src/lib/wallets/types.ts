// Common Wallet interface shared by Phantom and Ping. Both wallets
// hold a Solana ed25519 keypair and return base58-encoded address +
// signature shapes, so the iogrid-side bind logic doesn't need to
// know which app the user paired through except for the deeplink-back
// URL (top-up / re-sign).
//
// One Wallet implementation = one provider (`phantom` | `ping`).
// Track 2 of EPIC #581 (Closes #583 + #584). Track 5 ($GRID mint
// deploy) lands the actual SPL mint address; the wallet primitive
// here is mint-agnostic and looks up GRID_TOKEN_MINT from env at
// balance-fetch time.

export type WalletProvider = 'phantom' | 'ping';

/** The exact bytes the wallet must sign for an iogrid bind. */
export interface BindChallenge {
  /** Opaque nonce, hex string. */
  nonce: string;
  /** Server-issued unix timestamp (seconds). */
  timestamp: number;
  /** The wire form: "iogrid:bind:<nonce>:<timestamp>". */
  message: string;
}

/** Result returned by Wallet.connect / Wallet.signBindMessage. */
export interface WalletConnectResult {
  /** Base58-encoded Solana ed25519 pubkey. */
  address: string;
  /** Which wallet app produced the signature. */
  provider: WalletProvider;
  /** The challenge that was signed. */
  challenge: BindChallenge;
  /** Base58-encoded ed25519 signature of `challenge.message`. */
  signatureBase58: string;
}

/**
 * Shape every wallet adapter implements. Adapters are typically
 * thin wrappers around an iOS deeplink:
 *   - mobile opens `phantom://...` or `ping://...`
 *   - user approves in the wallet app
 *   - wallet returns to `iogrid://wallet-callback?...`
 *   - the adapter parses the callback + resolves the awaiting Promise.
 *
 * `iogrid:bind:<nonce>:<ts>` is the message we ALWAYS sign — neither
 * Phantom's signMessage nor Ping's deeplink touches that contract. The
 * adapter only handles the transport. The server (identity-svc
 * /v1/identity/wallet/bind) verifies the signature.
 */
export interface Wallet {
  readonly provider: WalletProvider;

  /**
   * Returns true if the wallet app is installed on this device.
   * Implemented via `Linking.canOpenURL("<scheme>://")`. iOS requires
   * the queried schemes to be listed under
   * `LSApplicationQueriesSchemes` in Info.plist — see app.json.
   */
  isInstalled(): Promise<boolean>;

  /**
   * Get / construct the deeplink the user follows to install the
   * wallet from the App Store. Surfaced when isInstalled() returns
   * false so the connect-wallet UI can render a "Get Phantom" button.
   */
  appStoreURL(): string;

  /**
   * Connect to the wallet AND obtain a signature over the iogrid
   * bind challenge. Caller is responsible for passing the challenge
   * built via {@link buildBindChallenge}; the wallet adapter only
   * routes the message through the wallet UI and decodes the response.
   *
   * Throws on user cancel, malformed callback, or transport error.
   * Caller maps errors to the connect-wallet "Try again" surface.
   */
  connectAndSign(challenge: BindChallenge): Promise<WalletConnectResult>;
}

/**
 * Build the exact bind challenge bytes a wallet must sign.
 * Format: `iogrid:bind:<hex_nonce>:<unix_seconds>` — must match
 * coordinator/services/identity-svc/internal/wallet.BuildChallenge
 * byte-for-byte; the server rejects anything else.
 *
 * Generates a 64-bit random nonce via expo-crypto (16 hex chars).
 */
export async function buildBindChallenge(): Promise<BindChallenge> {
  // Late import: expo-crypto is RN-only; importing at module load
  // would break the web stub. The path used here matches existing
  // src/lib/account.ts.
  const Crypto = await import('expo-crypto');
  const bytes = Crypto.getRandomBytes(8);
  let nonce = '';
  for (const b of bytes) nonce += b.toString(16).padStart(2, '0');
  const timestamp = Math.floor(Date.now() / 1000);
  return {
    nonce,
    timestamp,
    message: `iogrid:bind:${nonce}:${timestamp}`,
  };
}

/**
 * Truncate a base58 wallet address for UI display.
 *   "FwxQ…GhKp"  — first 4, last 4. Used by the wallet card.
 */
export function truncateAddress(address: string): string {
  if (address.length <= 10) return address;
  return `${address.slice(0, 4)}…${address.slice(-4)}`;
}
