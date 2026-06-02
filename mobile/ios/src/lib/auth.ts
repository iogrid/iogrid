// auth.ts — Sign in with Apple wrapper + session token persistence.
//
// Closes #582 (Track 1 of EPIC #581). The iogrid v1 mobile app uses
// Apple as the SOLE auth path on iOS — no email/password, no Google.
// Apple's native sheet returns an identity token (a JWT signed by
// Apple), we POST it to identity-svc, identity-svc validates against
// Apple's JWKS, creates or looks up the iogrid user, and returns a
// session JWT + refresh token. We persist both in iOS Keychain via
// expo-secure-store, scoped to the App Group so the NetworkExtension
// process can read them later.
//
// On second launch the cached JWT short-circuits the sign-in sheet
// (per #582 DoD): the app reads the token, checks the exp claim
// client-side, and either refreshes (>5min to go) or routes straight
// to the home screen.
//
// Error model: every public function in this module rejects with an
// `AuthError` whose `.code` is one of:
//   - `apple_canceled`       — user dismissed the native sheet
//   - `apple_failed`         — Apple returned a non-cancel error
//   - `server_rejected`      — identity-svc returned 4xx (token invalid)
//   - `server_unreachable`   — network / 5xx
// The UI maps `apple_canceled` to "no banner, just stay on the
// sign-in screen" and the rest to "Apple sign-in failed, try again".

import * as AppleAuthentication from 'expo-apple-authentication';
import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import Constants from 'expo-constants';

const SESSION_TOKEN_KEY = 'iogrid.auth.sessionToken';
const REFRESH_TOKEN_KEY = 'iogrid.auth.refreshToken';
const USER_ID_KEY = 'iogrid.auth.userId';
const WALLET_ADDRESS_KEY = 'iogrid.auth.walletAddress';

// Keychain access-group MUST match what NetworkExtension reads — same
// rationale as src/lib/account.ts.
const KEYCHAIN_ACCESS_GROUP = 'group.io.iogrid.app';

const KEYCHAIN_OPTIONS: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
  accessGroup: KEYCHAIN_ACCESS_GROUP,
};

const DEFAULT_BASE_URL = 'https://api.iogrid.org';

function baseURL(): string {
  const fromConfig = Constants.expoConfig?.extra?.coordinatorURL as string | undefined;
  return fromConfig ?? DEFAULT_BASE_URL;
}

export type AuthErrorCode =
  | 'apple_canceled'
  | 'apple_failed'
  | 'server_rejected'
  | 'server_unreachable';

export class AuthError extends Error {
  code: AuthErrorCode;
  constructor(code: AuthErrorCode, message: string) {
    super(message);
    this.name = 'AuthError';
    this.code = code;
  }
}

/** A loaded iogrid session — what every authenticated request needs. */
export interface Session {
  accessToken: string;
  refreshToken: string;
  /** iogrid user UUID returned in the JWT's `sub` claim. */
  userId: string;
  /**
   * Bound Solana wallet address (base58), or null when the user
   * hasn't connected one yet. Track 2 #583 wires the Connect-Wallet
   * flow that populates this.
   */
  walletAddress: string | null;
}

/**
 * Returns true iff the device supports Sign in with Apple. Always
 * true on iOS 13+; false on simulator without an Apple ID signed
 * into Settings, and false on Android / web platforms.
 */
export async function isAppleSignInAvailable(): Promise<boolean> {
  try {
    return await AppleAuthentication.isAvailableAsync();
  } catch {
    return false;
  }
}

/**
 * Drives the full Sign in with Apple flow:
 *
 *   1. Generate a cryptographically random nonce + its SHA-256 hash
 *   2. Open the native iOS sheet with `[FULL_NAME, EMAIL]` scopes
 *   3. POST the returned identityToken + nonce to identity-svc
 *   4. Persist the session bundle in Keychain
 *   5. Return the Session
 *
 * Throws AuthError on every failure path; callers handle the four
 * codes documented in the AuthErrorCode type.
 */
export async function signInWithApple(): Promise<Session> {
  // Nonce: random 32 bytes hex-encoded. Apple expects the nonce in
  // the sign-in request to be the SHA-256 of the value mixed into
  // the token's nonce claim. We send the RAW nonce as `nonce` and
  // Apple includes the SHA-256(raw) in the token; identity-svc then
  // compares the token claim against SHA-256(rawNonceWeJustSent).
  // For simplicity, we send + verify the SAME value end-to-end on
  // the client→server side and let Apple's sheet do the hashing.
  // The server's nonce check uses the raw value the client posts.
  const rawNonce = await randomNonce();

  let credential: AppleAuthentication.AppleAuthenticationCredential;
  try {
    credential = await AppleAuthentication.signInAsync({
      requestedScopes: [
        AppleAuthentication.AppleAuthenticationScope.FULL_NAME,
        AppleAuthentication.AppleAuthenticationScope.EMAIL,
      ],
      nonce: rawNonce,
    });
  } catch (err: unknown) {
    // expo-apple-authentication uses error code ERR_REQUEST_CANCELED
    // (Apple's iOS code 1001) when the user dismisses the sheet.
    const e = err as { code?: string; message?: string };
    if (
      e?.code === 'ERR_REQUEST_CANCELED' ||
      e?.code === 'ERR_CANCELED' ||
      String(e?.message ?? '').toLowerCase().includes('cancel')
    ) {
      throw new AuthError('apple_canceled', 'User dismissed the Apple sign-in sheet.');
    }
    throw new AuthError('apple_failed', `Apple sign-in failed: ${e?.message ?? 'unknown'}`);
  }

  if (!credential.identityToken) {
    throw new AuthError('apple_failed', 'Apple sheet returned no identityToken.');
  }

  // Compose fullName when Apple gave it to us (FIRST SIGN-IN ONLY —
  // subsequent sign-ins always have null fullName).
  const fullName = formatFullName(credential.fullName);

  // POST to identity-svc.
  let response: Response;
  try {
    response = await fetch(`${baseURL()}/v1/identity/apple-signin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({
        identity_token: credential.identityToken,
        nonce: rawNonce,
        apple_user: credential.user,
        full_name: fullName,
      }),
    });
  } catch (err: unknown) {
    throw new AuthError('server_unreachable', `Network error: ${(err as Error)?.message ?? 'unknown'}`);
  }
  if (response.status >= 500) {
    throw new AuthError('server_unreachable', `Server error: HTTP ${response.status}`);
  }
  if (!response.ok) {
    let detail = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { message?: string };
      if (body?.message) detail = body.message;
    } catch {
      /* ignore */
    }
    throw new AuthError('server_rejected', detail);
  }

  const body = (await response.json()) as {
    bundle?: {
      access_token: string;
      refresh_token: string;
      user?: { id?: string };
    };
    wallet_address?: string;
  };
  if (!body?.bundle?.access_token || !body?.bundle?.refresh_token) {
    throw new AuthError('server_rejected', 'Server returned no token bundle');
  }
  const userId = body.bundle.user?.id ?? '';
  const walletAddress = body.wallet_address ?? '';

  const session: Session = {
    accessToken: body.bundle.access_token,
    refreshToken: body.bundle.refresh_token,
    userId,
    walletAddress: walletAddress || null,
  };
  await persistSession(session);
  return session;
}

/**
 * Read a previously persisted session from Keychain. Returns null
 * if no session has been stored yet (fresh install) OR if the
 * stored access token has expired and a refresh hasn't been wired
 * (refresh is queued for a follow-up issue).
 */
export async function readPersistedSession(): Promise<Session | null> {
  const [accessToken, refreshToken, userId, walletAddress] = await Promise.all([
    SecureStore.getItemAsync(SESSION_TOKEN_KEY, KEYCHAIN_OPTIONS),
    SecureStore.getItemAsync(REFRESH_TOKEN_KEY, KEYCHAIN_OPTIONS),
    SecureStore.getItemAsync(USER_ID_KEY, KEYCHAIN_OPTIONS),
    SecureStore.getItemAsync(WALLET_ADDRESS_KEY, KEYCHAIN_OPTIONS),
  ]);
  if (!accessToken || !refreshToken || !userId) return null;
  if (isAccessTokenExpired(accessToken)) {
    // TODO(track-3): wire refresh exchange. For now treat as fresh launch.
    return null;
  }
  return {
    accessToken,
    refreshToken,
    userId,
    walletAddress: walletAddress || null,
  };
}

/** Drop the persisted session — invoked from /settings sign-out. */
export async function clearPersistedSession(): Promise<void> {
  await Promise.all([
    SecureStore.deleteItemAsync(SESSION_TOKEN_KEY, KEYCHAIN_OPTIONS),
    SecureStore.deleteItemAsync(REFRESH_TOKEN_KEY, KEYCHAIN_OPTIONS),
    SecureStore.deleteItemAsync(USER_ID_KEY, KEYCHAIN_OPTIONS),
    SecureStore.deleteItemAsync(WALLET_ADDRESS_KEY, KEYCHAIN_OPTIONS),
  ]);
}

// ── internals ─────────────────────────────────────────────────────

async function persistSession(s: Session): Promise<void> {
  await Promise.all([
    SecureStore.setItemAsync(SESSION_TOKEN_KEY, s.accessToken, KEYCHAIN_OPTIONS),
    SecureStore.setItemAsync(REFRESH_TOKEN_KEY, s.refreshToken, KEYCHAIN_OPTIONS),
    SecureStore.setItemAsync(USER_ID_KEY, s.userId, KEYCHAIN_OPTIONS),
    SecureStore.setItemAsync(WALLET_ADDRESS_KEY, s.walletAddress ?? '', KEYCHAIN_OPTIONS),
  ]);
}

async function randomNonce(): Promise<string> {
  const bytes = Crypto.getRandomBytes(32);
  // hex-encode — Apple accepts any string here; we just need it to
  // be unguessable + match between request + token claim.
  return Array.from(bytes).map((b) => b.toString(16).padStart(2, '0')).join('');
}

function formatFullName(name: AppleAuthentication.AppleAuthenticationFullName | null): string {
  if (!name) return '';
  const parts: string[] = [];
  if (name.givenName) parts.push(name.givenName);
  if (name.familyName) parts.push(name.familyName);
  return parts.join(' ');
}

// isAccessTokenExpired parses the JWT exp claim WITHOUT verifying the
// signature — we only need the timestamp to decide whether to skip the
// sign-in sheet on second launch. Any malformed token is treated as
// expired so the user re-authenticates rather than silently using a
// garbage token.
function isAccessTokenExpired(jwt: string): boolean {
  try {
    const [, payload] = jwt.split('.');
    if (!payload) return true;
    const decoded = JSON.parse(base64UrlDecode(payload));
    const exp = decoded?.exp;
    if (typeof exp !== 'number') return true;
    const nowSec = Math.floor(Date.now() / 1000);
    // 60s skew margin — refresh slightly before exp so the next call
    // doesn't race the expiry.
    return nowSec + 60 >= exp;
  } catch {
    return true;
  }
}

function base64UrlDecode(s: string): string {
  // Pad to a length divisible by 4 and convert URL-safe alphabet.
  const padded = s.replace(/-/g, '+').replace(/_/g, '/');
  const pad = padded.length % 4;
  const full = pad ? padded + '='.repeat(4 - pad) : padded;
  // React Native polyfills atob via JSC; in Hermes 0.74+ it's available.
  // We use Buffer if present, atob otherwise.
  if (typeof atob === 'function') return atob(full);
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const { Buffer } = require('buffer');
  return Buffer.from(full, 'base64').toString('binary');
}
