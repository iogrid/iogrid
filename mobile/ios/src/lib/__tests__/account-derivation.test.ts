// Tests for the Mullvad-model identity derivation (Refs #569).
//
// digestToUuidV4 is the load-bearing function of the entire anonymous-
// account scheme: the account number's SHA-256 → the customer_id the
// coordinator keys every row on. "Recovery on a new device" works ONLY
// because this derivation is byte-for-byte deterministic — type the same
// number, get the same UUID, find your rows. If the slice indices or the
// RFC-4122 version/variant bit-patching ever drift, every existing user
// silently re-derives a DIFFERENT id on their next app update and loses
// their identity + sessions. That regression is invisible at runtime and
// catastrophic — so the exact output is pinned here with literal vectors.
//
// All three helpers are pure (no Keychain, no crypto) — the SHA-256 is
// done by the caller and the hex passed in — so they run under node
// directly. (account.ts itself can't be imported in jest: it pulls
// expo-secure-store, which has no mock.)

import {
  accountNumberFromBytes,
  digestToUuidV4,
  formatAccountNumber,
} from '../account-derivation';

describe('formatAccountNumber', () => {
  it('groups 16 digits into 4×4 with single spaces (display only)', () => {
    expect(formatAccountNumber('1234567890123456')).toBe('1234 5678 9012 3456');
  });

  it('does not trail a space after the final group', () => {
    expect(formatAccountNumber('12345678')).toBe('1234 5678');
    expect(formatAccountNumber('1234')).toBe('1234');
  });
});

describe('accountNumberFromBytes', () => {
  it('always returns exactly 16 digits, zero-padded', () => {
    expect(accountNumberFromBytes([0, 0, 0, 0, 0, 0, 0, 0])).toBe('0000000000000000');
    expect(accountNumberFromBytes([0, 0, 0, 0, 0, 0, 0, 1])).toBe('0000000000000001');
    expect(accountNumberFromBytes([0, 0, 0, 0, 0, 0, 0, 1])).toHaveLength(16);
  });

  it('interprets the bytes as a 64-bit big-endian integer mod 10^16', () => {
    // 0xFF×8 = 2^64-1 = 18446744073709551615; mod 10^16 = last 16 digits.
    expect(accountNumberFromBytes([255, 255, 255, 255, 255, 255, 255, 255])).toBe(
      '6744073709551615',
    );
    // big-endian: high byte first → 0x01 in the MSB position is 2^56.
    expect(accountNumberFromBytes([1, 0, 0, 0, 0, 0, 0, 0])).toBe(
      (72057594037927936n % 10_000_000_000_000_000n).toString().padStart(16, '0'),
    );
  });

  it('accepts a Uint8Array (the real getRandomBytes return type)', () => {
    expect(accountNumberFromBytes(new Uint8Array([0, 0, 0, 0, 0, 0, 0, 42]))).toBe(
      '0000000000000042',
    );
  });
});

describe('digestToUuidV4 — the recovery invariant', () => {
  it('pins the all-zero digest', () => {
    expect(digestToUuidV4('0'.repeat(64))).toBe('00000000-0000-4000-8000-000000000000');
  });

  it('pins the all-f digest (variant nibble patches f→b)', () => {
    expect(digestToUuidV4('f'.repeat(64))).toBe('ffffffff-ffff-4fff-bfff-ffffffffffff');
  });

  it('pins a realistic mixed digest', () => {
    const digest = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
    expect(digestToUuidV4(digest)).toBe('01234567-89ab-4def-8123-456789abcdef');
  });

  it('emits a structurally valid UUIDv4 (version 4, variant 8/9/a/b)', () => {
    const id = digestToUuidV4('7a3f9c1e8b2d6045e1f0c3a7b9d4e2f60123456789abcdef0123456789abcdef');
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
  });

  it('is deterministic + case-normalizing (the same number always recovers the same id)', () => {
    const digest = 'ABCDEF0123456789abcdef0123456789ABCDEF0123456789abcdef0123456789';
    const a = digestToUuidV4(digest);
    const b = digestToUuidV4(digest.toLowerCase());
    expect(a).toBe(b); // upper/lower input → identical id
    expect(a).toBe(digestToUuidV4(digest)); // repeatable
    expect(a).toBe(a.toLowerCase()); // output is lowercase
  });

  it('uses only the first 16 bytes of the digest (trailing bytes are ignored)', () => {
    const head = '0123456789abcdef0123456789abcdef';
    expect(digestToUuidV4(head + '0'.repeat(32))).toBe(digestToUuidV4(head + 'f'.repeat(32)));
  });
});
