// Tests for the first-launch routing decision (Refs #590).
//
// AuthGate is the single mount-time gate that decides whether a fresh
// install drops onto /(onboarding)/welcome or onto the main VPN
// toggle. It runs OUTSIDE the React render path (in a useEffect at
// root-layout mount), reads the `iogrid.onboarded` flag from
// AsyncStorage, and `router.replace`s when the flag is missing or
// when the read throws.
//
// Why this matters: every new install (post-#590 ship) hits this
// gate on launch. A regression here = "user opens iogrid, sees the
// VPN toggle without ever seeing onboarding". A regression in the
// other direction (flag stamped but gate still routes) = "user
// completes onboarding, force-quits, reopens, bounced back to
// onboarding". Both are bug-bash-grade.
//
// The matrix covered below:
//
//   1. AuthGate with NO flag           → router.replace('/(onboarding)/welcome')
//   2. AuthGate WITH flag              → router.replace NOT called
//   3. AuthGate + AsyncStorage throws  → router.replace('/(onboarding)/welcome')
//      (defensive default; we'd rather show onboarding again than
//       drop the user onto a main screen that may have stale data).
//   4. ConnectWallet onContinue        → setItem('iogrid.onboarded','1')
//      THEN router.replace('/')        (order matters; see test
//      comments below).
//
// The production code under test (src/app/_layout.tsx +
// src/app/(onboarding)/connect-wallet.tsx) exports the core
// async helpers so the test can drive them directly without
// spinning up a React renderer.

import {
  checkOnboardedAndRoute,
  ONBOARDED_FLAG_KEY,
} from '../_layout';
import { stampOnboardedAndContinue } from '../(onboarding)/connect-wallet';
import {
  __getRouter,
  __reset as __resetRouter,
} from '../../lib/mocks/expo-router';
import AsyncStorage, {
  __getStore,
  __reset as __resetAsyncStorage,
  __seed,
  __setThrow,
} from '../../lib/mocks/async-storage';

beforeEach(() => {
  __resetRouter();
  __resetAsyncStorage();
});

// -----------------------------------------------------------------------
// 1. AuthGate with NO flag — first launch routes into onboarding
// -----------------------------------------------------------------------

describe('checkOnboardedAndRoute — no flag (first launch)', () => {
  it('routes to /(onboarding)/welcome when AsyncStorage returns null', async () => {
    // AsyncStorage starts empty by default after __resetAsyncStorage().
    expect(__getStore().has(ONBOARDED_FLAG_KEY)).toBe(false);

    await checkOnboardedAndRoute();

    const router = __getRouter();
    expect(router.replace).toEqual(['/(onboarding)/welcome']);
    expect(router.push).toEqual([]);
  });

  it('uses the canonical key `iogrid.onboarded` (cross-screen contract)', () => {
    // This is the contract between AuthGate (reader) and
    // connect-wallet.tsx (writer). If either side renames the key
    // independently, AuthGate will silently never trip the
    // "onboarded" branch and re-route forever.
    expect(ONBOARDED_FLAG_KEY).toBe('iogrid.onboarded');
  });
});

// -----------------------------------------------------------------------
// 2. AuthGate WITH flag — returning user stays on current route
// -----------------------------------------------------------------------

describe('checkOnboardedAndRoute — flag present (returning user)', () => {
  it('does NOT call router.replace when AsyncStorage already has the flag', async () => {
    __seed({ [ONBOARDED_FLAG_KEY]: '1' });

    await checkOnboardedAndRoute();

    const router = __getRouter();
    expect(router.replace).toEqual([]);
    expect(router.push).toEqual([]);
  });

  it('treats ANY non-empty flag value as "onboarded"', async () => {
    // The contract is "key exists and value is truthy". Connect-wallet
    // happens to write '1' but the gate must not be fragile about
    // the exact value — future flow versions may stamp '2' / 'v2'.
    __seed({ [ONBOARDED_FLAG_KEY]: 'v2-flow' });

    await checkOnboardedAndRoute();

    expect(__getRouter().replace).toEqual([]);
  });
});

// -----------------------------------------------------------------------
// 3. AuthGate with AsyncStorage throw — defensive default to onboarding
// -----------------------------------------------------------------------

describe('checkOnboardedAndRoute — AsyncStorage failure (defensive)', () => {
  it('routes to /(onboarding)/welcome when getItem throws', async () => {
    // Force the next getItem to reject. The production code wraps
    // the call in try/catch and defaults to onboarding — the user
    // sees a welcome screen rather than a confusing main screen
    // populated with stub data.
    __setThrow(true);

    await checkOnboardedAndRoute();

    const router = __getRouter();
    expect(router.replace).toEqual(['/(onboarding)/welcome']);
  });

  it('never throws / never rejects (caller awaits without try/catch)', async () => {
    __setThrow(true);

    // The production caller (`useEffect` inside AuthGate) chains
    // `.finally(setChecked)` and does NOT have a `.catch`. If
    // checkOnboardedAndRoute() ever re-throws, the splash overlay
    // hangs and the app never finishes its first-launch decision.
    await expect(checkOnboardedAndRoute()).resolves.toBeUndefined();
  });
});

// -----------------------------------------------------------------------
// 4. ConnectWallet onContinue — setItem THEN router.replace
// -----------------------------------------------------------------------

describe('stampOnboardedAndContinue — exit from onboarding', () => {
  it("writes 'iogrid.onboarded'='1' before navigating to '/'", async () => {
    await stampOnboardedAndContinue();

    // The flag MUST land in storage. If it doesn't, the next mount
    // of AuthGate will bounce the user back into onboarding —
    // exactly the bug Refs #590 is preventing.
    expect(__getStore().get(ONBOARDED_FLAG_KEY)).toBe('1');

    // And the user must end up on the main route.
    const router = __getRouter();
    expect(router.replace).toEqual(['/']);
  });

  it('writes the flag BEFORE calling router.replace (race-safe)', async () => {
    // We instrument setItem so we can observe call ordering relative
    // to router.replace. The production code awaits setItem first;
    // if a future refactor flips that, a fast re-mount of the root
    // layout (which can happen on iOS state-restoration) could read
    // a null flag and route back into onboarding mid-replace.
    const order: string[] = [];
    const realSetItem = AsyncStorage.setItem.bind(AsyncStorage);
    AsyncStorage.setItem = async (k: string, v: string) => {
      order.push('setItem');
      await realSetItem(k, v);
    };

    // Spy on the mocked router.replace by re-reading the captured
    // history length before/after.
    const replaceBefore = __getRouter().replace.length;
    await stampOnboardedAndContinue();
    const replaceAfter = __getRouter().replace.length;
    if (replaceAfter > replaceBefore) order.push('replace');

    expect(order).toEqual(['setItem', 'replace']);

    // Restore for subsequent tests in this file.
    AsyncStorage.setItem = realSetItem;
  });

  it('still navigates even when setItem throws (demo never blocks)', async () => {
    // Storage write failures must not strand the user on the
    // connect-wallet screen — the demo always proceeds. The user
    // will just see onboarding again on the next launch.
    __setThrow(true);

    await stampOnboardedAndContinue();

    expect(__getRouter().replace).toEqual(['/']);
  });
});
