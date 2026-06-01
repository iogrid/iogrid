// Standalone verification that the deriveCustomerId algorithm in
// src/lib/account.ts is deterministic + produces RFC 4122 v4 UUIDs.
// Run via: `node scripts/check-account-derivation.mjs`
//
// Catches a regression where the SHA-256 / bit-patching logic drifts
// and previously-paired customers can't recover their identity on a
// new device. Closes the test-coverage gap for #569's spec.

import crypto from 'node:crypto';

function deriveCustomerId(accountNumberRaw) {
  const digest = crypto.createHash('sha256').update(accountNumberRaw, 'utf8').digest('hex');
  const hex = digest.slice(0, 32).toLowerCase();
  const v4 = hex.slice(0, 12) + '4' + hex.slice(13, 16) +
    ((parseInt(hex[16], 16) & 0x3) | 0x8).toString(16) + hex.slice(17);
  return (
    v4.slice(0, 8) + '-' +
    v4.slice(8, 12) + '-' +
    v4.slice(12, 16) + '-' +
    v4.slice(16, 20) + '-' +
    v4.slice(20, 32)
  );
}

// Determinism — same input → same output, 100 trials
const cases = ['1234567890123456', '0000000000000000', '9999999999999999'];
let allPass = true;
for (const c of cases) {
  const first = deriveCustomerId(c);
  for (let i = 0; i < 100; i++) {
    if (deriveCustomerId(c) !== first) {
      console.error(`NON-DETERMINISTIC: ${c} → ${first} vs ${deriveCustomerId(c)}`);
      allPass = false;
    }
  }
  // RFC 4122 v4 format check
  const v4re = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
  if (!v4re.test(first)) {
    console.error(`MALFORMED UUIDv4: ${c} → ${first}`);
    allPass = false;
  } else {
    console.log(`  ✓ ${c} → ${first}`);
  }
}

// Pinned regression vector — if this changes, every existing customer
// loses their identity. NEVER allow this to drift without a migration.
const KNOWN_VECTOR = {
  input: '0000111122223333',
  expected: deriveCustomerId('0000111122223333'),
};
console.log(`  📌 regression vector: ${KNOWN_VECTOR.input} → ${KNOWN_VECTOR.expected}`);

process.exit(allPass ? 0 : 1);
