// Mullvad-style anonymous account ID — the entire identity model for
// the iogrid iOS VPN app v1. No email, no password, no OAuth, no
// magic link. Closes #569.
//
// First launch:
//   1. Generate a random 16-digit account number (formatted as
//      `1234 5678 9012 3456` — Mullvad UX convention; the spaces
//      are display-only, NOT stored)
//   2. Derive a stable UUIDv4 from the account number via SHA-256
//      (first 16 bytes of the digest, formatted as a UUID). The
//      derivation is one-way + deterministic, so the same account
//      number always yields the same customer_id — that's how a
//      user typing the number on a new device recovers their
//      identity.
//   3. Persist both in iOS Keychain via expo-secure-store with
//      `keychainAccessible: WHEN_UNLOCKED_THIS_DEVICE_ONLY` (so the
//      key never syncs to iCloud Keychain — VPN credentials must
//      not leak to Apple's cloud) and `keychainAccessGroup` set to
//      the App Group identifier so the NetworkExtension process
//      (which runs in its own sandbox) can read the same record.
//
// Subsequent launches:
//   - Read from Keychain. If missing, regenerate (silently — this
//     is a fresh-install signal).
//
// "Recovery on new device" flow (deferred to a later issue, scaffold
// only here):
//   - User types their account number on the new device, app derives
//     the same customer_id, fetches their session state from the
//     coordinator, restores.

import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';

import {
  accountNumberFromBytes,
  digestToUuidV4,
  formatAccountNumber,
} from '@/lib/account-derivation';

const ACCOUNT_NUMBER_KEY = 'iogrid.account.number';
const CUSTOMER_ID_KEY = 'iogrid.account.customerId';

// Keychain access-group MUST match the App Group identifier declared
// in app.json + PacketTunnelProvider.entitlements. Without this, the
// NE extension process can't read the same Keychain entry the main
// app writes — and we'd have to send the customer_id over the
// providerConfiguration IPC instead (less secure, more plumbing).
const KEYCHAIN_ACCESS_GROUP = 'group.io.iogrid.app';

const KEYCHAIN_OPTIONS: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
  accessGroup: KEYCHAIN_ACCESS_GROUP,
};

/** A loaded iogrid identity — what every coordinator call needs. */
export interface Identity {
  /** Display string: "1234 5678 9012 3456" (spaces every 4 digits). */
  accountNumberDisplay: string;
  /** Raw 16-digit string without spaces — never shown to the user. */
  accountNumberRaw: string;
  /** UUIDv4 derived from accountNumberRaw — sent as customer_id. */
  customerId: string;
}

/**
 * Load the existing identity from Keychain, or generate + persist a
 * fresh one if none exists. Idempotent — safe to call on every app
 * launch.
 */
export async function loadOrCreateIdentity(): Promise<Identity> {
  const existing = await readKeychain();
  if (existing) return existing;

  const raw = generateAccountNumber();
  const customerId = await deriveCustomerId(raw);
  const identity: Identity = {
    accountNumberRaw: raw,
    accountNumberDisplay: formatAccountNumber(raw),
    customerId,
  };
  await writeKeychain(identity);
  return identity;
}

/**
 * Read the identity from Keychain. Returns null if either record is
 * missing — callers should treat that as "fresh install" and call
 * loadOrCreateIdentity to regenerate.
 */
async function readKeychain(): Promise<Identity | null> {
  const [raw, customerId] = await Promise.all([
    SecureStore.getItemAsync(ACCOUNT_NUMBER_KEY, KEYCHAIN_OPTIONS),
    SecureStore.getItemAsync(CUSTOMER_ID_KEY, KEYCHAIN_OPTIONS),
  ]);
  if (!raw || !customerId) return null;
  return {
    accountNumberRaw: raw,
    accountNumberDisplay: formatAccountNumber(raw),
    customerId,
  };
}

/** Persist identity to Keychain. Both writes are concurrent. */
async function writeKeychain(identity: Identity): Promise<void> {
  await Promise.all([
    SecureStore.setItemAsync(ACCOUNT_NUMBER_KEY, identity.accountNumberRaw, KEYCHAIN_OPTIONS),
    SecureStore.setItemAsync(CUSTOMER_ID_KEY, identity.customerId, KEYCHAIN_OPTIONS),
  ]);
}

/**
 * Generate a 16-digit decimal account number from a 64-bit random
 * source. Why 16 digits: matches Mullvad's UX convention exactly.
 * Why decimal not hex: typing alpha characters on a phone keyboard is
 * recovery friction we don't need to introduce.
 *
 * Entropy: 16 decimal digits = ~53 bits. Collisions are
 * astronomically unlikely at our user count + the coordinator
 * de-dupes by customer_id (UUIDv4 derived from the number) on a
 * shared Postgres unique index, so a collision surfaces as a
 * graceful "regenerate" rather than data corruption.
 */
function generateAccountNumber(): string {
  // Pure 64-bit-BE → 16-digit-decimal math lives in account-derivation
  // (unit-tested); here we just feed it fresh entropy.
  return accountNumberFromBytes(Crypto.getRandomBytes(8));
}

/**
 * Derive a stable customer UUID from the account number using SHA-256.
 *
 * Why deterministic: the user can type their account number on a new
 * device and recover their identity without server roundtrip — the
 * client locally derives the same UUID, then the coordinator
 * (which only sees UUIDs) looks up their existing rows.
 *
 * Why SHA-256 not HKDF/PBKDF2: the account number IS the secret, not
 * a low-entropy password. Adding KDF iterations doesn't materially
 * raise the bar against a brute-force attacker who can ask the
 * coordinator "is this account number valid" — coordinator rate
 * limiting is the real defense.
 *
 * UUID formatting: take the first 16 bytes (128 bits) of the
 * SHA-256 output, format as 8-4-4-4-12 hex with the standard UUID
 * version (0x4) + variant (0x8) bit patches so identity-svc
 * recognises it as a valid UUIDv4 string.
 */
async function deriveCustomerId(accountNumberRaw: string): Promise<string> {
  const digest = await Crypto.digestStringAsync(
    Crypto.CryptoDigestAlgorithm.SHA256,
    accountNumberRaw,
    { encoding: Crypto.CryptoEncoding.HEX },
  );
  // RFC-4122 formatting (the recovery invariant) lives in
  // account-derivation, pinned by a unit-test vector.
  return digestToUuidV4(digest);
}
