// Jest mock for `@react-native-async-storage/async-storage`.
//
// The real package ships a TurboModule that talks to NSUserDefaults
// (iOS) / SharedPreferences (Android) via the bridge and crashes on
// import under plain Node. The mock here implements a tiny in-memory
// Map that's good enough for the AuthGate / onboarding tests:
//
//   - getItem(key)       → returns stored string | null
//   - setItem(key, val)  → writes
//   - removeItem(key)    → deletes
//   - clear()            → drops everything
//
// Tests can:
//   1. Pre-populate state via __seed({ key: value }) before the call
//      under test runs (covers the "flag already set" branch).
//   2. Force getItem/setItem to throw via __setThrow(true) (covers the
//      defensive try/catch path for storage corruption).
//   3. Reset between tests via __reset().

let store = new Map<string, string>();
let throwOnNext = false;

const AsyncStorage = {
  async getItem(key: string): Promise<string | null> {
    if (throwOnNext) {
      throw new Error('async-storage: simulated read failure');
    }
    return store.has(key) ? store.get(key)! : null;
  },
  async setItem(key: string, value: string): Promise<void> {
    if (throwOnNext) {
      throw new Error('async-storage: simulated write failure');
    }
    store.set(key, value);
  },
  async removeItem(key: string): Promise<void> {
    store.delete(key);
  },
  async clear(): Promise<void> {
    store.clear();
  },
  async getAllKeys(): Promise<readonly string[]> {
    return Array.from(store.keys());
  },
  async multiGet(
    keys: readonly string[],
  ): Promise<ReadonlyArray<readonly [string, string | null]>> {
    return keys.map((k) => [k, store.has(k) ? store.get(k)! : null] as const);
  },
};

export default AsyncStorage;

// -----------------------------------------------------------------------
// Test helpers (not part of the production API surface)
// -----------------------------------------------------------------------

export function __seed(initial: Record<string, string>): void {
  store = new Map(Object.entries(initial));
}

export function __setThrow(value: boolean): void {
  throwOnNext = value;
}

export function __getStore(): ReadonlyMap<string, string> {
  return store;
}

export function __reset(): void {
  store = new Map();
  throwOnNext = false;
}
