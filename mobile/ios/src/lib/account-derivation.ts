/**
 * Pure identity-derivation helpers for the Mullvad-style anonymous
 * account model (#569). Split out of account.ts so they can be unit-
 * tested: account.ts imports `expo-secure-store` (Keychain) which has no
 * jest mock, making the whole module un-importable under node — but the
 * derivation math itself touches no native API.
 *
 * Why this matters enough to test: `digestToUuidV4` turns the account
 * number's SHA-256 into the `customer_id` the coordinator keys every row
 * on. The derivation is the ENTIRE recovery story — typing your number on
 * a new device works only because the same number deterministically
 * yields the same UUID. A drift in the slice indices or the RFC-4122
 * version/variant bit-patching silently re-derives a DIFFERENT id for
 * every existing user → everyone loses their identity + sessions on the
 * next app update. That regression is invisible without a pinned vector.
 *
 * Refs #569, #580.
 */

/** Format "1234567890123456" as "1234 5678 9012 3456" (display-only). */
export function formatAccountNumber(raw: string): string {
  return raw.replace(/(\d{4})(?=\d)/g, '$1 ');
}

/**
 * Derive the 16-digit decimal account number from an 8-byte random
 * source: interpret the bytes as a 64-bit big-endian unsigned integer,
 * take it modulo 10^16, and zero-pad to exactly 16 digits. Always 16
 * digits (the padStart guarantees leading zeros are kept).
 */
export function accountNumberFromBytes(bytes: Uint8Array | ArrayLike<number>): string {
  let n = 0n;
  for (let i = 0; i < bytes.length; i++) n = (n << 8n) + BigInt(bytes[i]);
  const sixteenDigits = n % 10_000_000_000_000_000n;
  return sixteenDigits.toString().padStart(16, '0');
}

/**
 * Format a SHA-256 hex digest as a valid UUIDv4 string. Takes the first
 * 16 bytes (32 hex chars) of the digest and patches the RFC-4122 version
 * (nibble 13 → '4') and variant (nibble 17 → 8/9/a/b) bits so
 * identity-svc accepts it as a UUIDv4. Deterministic + pure: the same
 * digest always yields the same id (the recovery invariant).
 */
export function digestToUuidV4(digestHex: string): string {
  // digest is ≥64 hex chars (32 bytes from SHA-256); use the first 16 bytes.
  const hex = digestHex.slice(0, 32).toLowerCase();
  // Patch version (4) + variant (8/9/a/b) bits per RFC 4122.
  const v4 =
    hex.slice(0, 12) +
    '4' +
    hex.slice(13, 16) +
    ((parseInt(hex[16], 16) & 0x3) | 0x8).toString(16) +
    hex.slice(17);
  return (
    v4.slice(0, 8) +
    '-' +
    v4.slice(8, 12) +
    '-' +
    v4.slice(12, 16) +
    '-' +
    v4.slice(16, 20) +
    '-' +
    v4.slice(20, 32)
  );
}
