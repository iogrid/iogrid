// Jest mock for `expo-crypto`. Production reaches getRandomBytes()
// via a dynamic import (see src/lib/wallets/types.ts), so we expose
// the same name and back it with node's crypto.randomBytes.

import { randomBytes } from 'crypto';

export function getRandomBytes(n: number): Uint8Array {
  return new Uint8Array(randomBytes(n));
}
