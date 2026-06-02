// Tests for Phantom + Ping wallet adapters (Refs #583, #584, #585).
//
// Coverage targets these production-bug shapes documented in the EPIC
// #581 cherry-pick PR #602 audit:
//
//   1. Phantom deeplink with malformed NaCl-box payload — wallet
//      returns garbage in `data=` → adapter must surface a clean
//      "phantom: connect decrypt failed" error, not crash.
//   2. Phantom session-token reuse across launches — once a connect
//      flow is in flight, a re-entrant connectAndSign must reject
//      cleanly; foreign-scheme deeplinks must not disturb the pending
//      session.
//   3. Ping app-not-installed — Linking.canOpenURL('ping://') returns
//      false → isInstalled() resolves false (clean fallback path).
//
// IMPLEMENTATION NOTE — module-level singletons.
//
// Both adapters use a `let pending: Pending | null` at module scope
// to track the in-flight connect flow + a `let linkingSub` to ensure
// the deeplink listener is subscribed exactly once per process. To
// keep tests isolated we re-require the modules per test (via
// jest.isolateModules) so each test starts with a freshly-initialised
// pending=null + sub=null. The expo-linking mock is also re-required
// in the same isolated module graph so its listener registry maps to
// the same adapter instance under test.

import bs58 from 'bs58';
import nacl from 'tweetnacl';

import type { BindChallenge, Wallet } from '../types';

const REDIRECT = 'iogrid://wallet-callback';

function makeChallenge(): BindChallenge {
  const nonce = 'deadbeefcafef00d';
  const timestamp = 1717000000;
  return {
    nonce,
    timestamp,
    message: `iogrid:bind:${nonce}:${timestamp}`,
  };
}

/**
 * Load `phantomWallet` / `pingWallet` + the expo-linking mock helpers
 * in a freshly-reset module graph so every test sees a clean pending
 * state. Calls `cb(modules)` synchronously inside `jest.isolateModules`.
 */
function withFreshModules<T>(
  cb: (m: {
    phantomWallet: Wallet;
    pingWallet: Wallet;
    mock: typeof import('../../mocks/expo-linking');
  }) => T,
): T {
  let result!: T;
  jest.isolateModules(() => {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const mock = require('../../mocks/expo-linking') as typeof import('../../mocks/expo-linking');
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const phantom = require('../phantom') as { phantomWallet: Wallet };
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const ping = require('../ping') as { pingWallet: Wallet };
    mock.__reset();
    result = cb({
      phantomWallet: phantom.phantomWallet,
      pingWallet: ping.pingWallet,
      mock,
    });
  });
  return result;
}

// -----------------------------------------------------------------------
// 1. Phantom — malformed NaCl-box payload
// -----------------------------------------------------------------------

describe('phantomWallet — malformed callback payload', () => {
  it('rejects with "connect decrypt failed" when data is garbage ciphertext', async () => {
    await withFreshModules(async ({ phantomWallet, mock }) => {
      const flow = phantomWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      // Confirm the adapter launched the connect deeplink.
      const launched = mock.__getOpenHistory().at(-1) ?? '';
      expect(launched.startsWith('phantom://v1/connect')).toBe(true);

      // Forge a Phantom callback with a *valid-shape* phantom pubkey +
      // nonce but a garbage ciphertext. `nacl.box.open.after` should
      // return null and the adapter should reject cleanly.
      const phantomKp = nacl.box.keyPair();
      const cb =
        `${REDIRECT}?` +
        new URLSearchParams({
          phantom_encryption_public_key: bs58.encode(phantomKp.publicKey),
          nonce: bs58.encode(nacl.randomBytes(24)),
          data: bs58.encode(nacl.randomBytes(48)),
        }).toString();
      mock.__fireUrl(cb);

      await expect(flow).rejects.toThrow(/phantom: connect decrypt failed/);
    });
  });

  it('rejects with "connect callback missing fields" when phantom drops a required param', async () => {
    await withFreshModules(async ({ phantomWallet, mock }) => {
      const flow = phantomWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      // No phantom_encryption_public_key, no nonce, no data.
      mock.__fireUrl(`${REDIRECT}?`);

      await expect(flow).rejects.toThrow(/connect callback missing fields/);
    });
  });

  it('surfaces phantom-provided errorCode without crashing', async () => {
    await withFreshModules(async ({ phantomWallet, mock }) => {
      const flow = phantomWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({
            errorCode: '4001',
            errorMessage: 'user rejected',
          }).toString(),
      );

      await expect(flow).rejects.toThrow(/phantom: user rejected/);
    });
  });
});

// -----------------------------------------------------------------------
// 2. Phantom — session-token / multi-launch isolation
// -----------------------------------------------------------------------

describe('phantomWallet — session-token + multi-launch isolation', () => {
  it('refuses to start a second connect flow while one is pending', async () => {
    await withFreshModules(async ({ phantomWallet, mock }) => {
      const first = phantomWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      // Re-entrant call MUST reject — would clobber the pending resolver
      // + leak the session.
      await expect(
        phantomWallet.connectAndSign(makeChallenge()),
      ).rejects.toThrow(/another connect flow is already in progress/);

      // Drain the first flow.
      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({ errorCode: 'cleanup' }).toString(),
      );
      await expect(first).rejects.toThrow();
    });
  });

  it('ignores callbacks whose redirect prefix does not match (foreign deeplinks)', async () => {
    await withFreshModules(async ({ phantomWallet, mock }) => {
      const flow = phantomWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      // Foreign-scheme callback must NOT disturb the pending flow.
      mock.__fireUrl('someotherapp://wallet-callback?address=stolen');

      // Fire a real error to drain — if the foreign URL had cleared
      // pending, this would be ignored and the test would time out.
      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({
            errorCode: '4001',
            errorMessage: 'user rejected',
          }).toString(),
      );

      await expect(flow).rejects.toThrow(/phantom: user rejected/);
    });
  });

  it('drops stale callbacks arriving with no pending flow', () => {
    withFreshModules(({ mock }) => {
      // No connectAndSign call — fire a phantom-shaped callback and
      // confirm the listener is a no-op (the `if (!pending) return`
      // short-circuit in the production listener).
      expect(() => {
        mock.__fireUrl(
          `${REDIRECT}?` +
            new URLSearchParams({
              phantom_encryption_public_key: bs58.encode(
                nacl.box.keyPair().publicKey,
              ),
              nonce: bs58.encode(nacl.randomBytes(24)),
              data: bs58.encode(nacl.randomBytes(48)),
            }).toString(),
        );
      }).not.toThrow();
    });
  });

  it('constructs a connect URL with the documented Phantom v1 protocol fields', async () => {
    await withFreshModules(async ({ phantomWallet, mock }) => {
      const flow = phantomWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      const launched = mock.__getOpenHistory().at(-1) ?? '';
      expect(launched).toMatch(/^phantom:\/\/v1\/connect\?/);
      const query = launched.slice('phantom://v1/connect?'.length);
      const params = new URLSearchParams(query);
      expect(params.get('dapp_encryption_public_key')).toBeTruthy();
      expect(params.get('cluster')).toMatch(/^(mainnet-beta|devnet)$/);
      expect(params.get('app_url')).toBe('https://iogrid.org');
      expect(params.get('redirect_link')).toBe(REDIRECT);

      // Drain.
      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({ errorCode: 'cleanup' }).toString(),
      );
      await expect(flow).rejects.toThrow();
    });
  });
});

// -----------------------------------------------------------------------
// 3. Ping — app-not-installed clean fallback
// -----------------------------------------------------------------------

describe('pingWallet — install detection + deeplink shape', () => {
  it('returns false from isInstalled() when canOpenURL(ping://) is false', async () => {
    await withFreshModules(async ({ pingWallet, mock }) => {
      mock.__setCanOpen('ping://', false);
      await expect(pingWallet.isInstalled()).resolves.toBe(false);
    });
  });

  it('returns true from isInstalled() when ping is registered', async () => {
    await withFreshModules(async ({ pingWallet, mock }) => {
      mock.__setCanOpen('ping://', true);
      await expect(pingWallet.isInstalled()).resolves.toBe(true);
    });
  });

  it('provides an App Store URL the connect-wallet UI can fall back to', () => {
    withFreshModules(({ pingWallet }) => {
      const url = pingWallet.appStoreURL();
      expect(url).toMatch(/^https:\/\/apps\.apple\.com\/app\/ping/);
    });
  });

  it('builds a ping://wallet/connect URL carrying the bind challenge', async () => {
    await withFreshModules(async ({ pingWallet, mock }) => {
      const challenge = makeChallenge();
      const flow = pingWallet.connectAndSign(challenge);
      await Promise.resolve();

      const launched = mock.__getOpenHistory().at(-1) ?? '';
      expect(launched.startsWith('ping://wallet/connect?')).toBe(true);
      const query = launched.slice('ping://wallet/connect?'.length);
      const params = new URLSearchParams(query);
      expect(params.get('app')).toBe('iogrid');
      expect(params.get('redirect')).toBe(REDIRECT);
      expect(params.get('challenge')).toBe(challenge.message);

      // Drain with a synthetic error so the pending Promise resolves.
      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({ source: 'ping', error: 'cleanup' }).toString(),
      );
      await expect(flow).rejects.toThrow(/ping: cleanup/);
    });
  });

  it('ignores callbacks without source=ping (Phantom-flow callbacks)', async () => {
    await withFreshModules(async ({ pingWallet, mock }) => {
      const flow = pingWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      // Phantom-shaped callback (no source=ping) must not disturb the
      // ping flow — the two adapters share the same redirect URL.
      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({
            phantom_encryption_public_key: 'whatever',
          }).toString(),
      );

      // Drain with a proper ping error.
      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({
            source: 'ping',
            error: 'user rejected',
          }).toString(),
      );
      await expect(flow).rejects.toThrow(/ping: user rejected/);
    });
  });

  it('rejects when ping callback is missing address/signature', async () => {
    await withFreshModules(async ({ pingWallet, mock }) => {
      const flow = pingWallet.connectAndSign(makeChallenge());
      await Promise.resolve();

      mock.__fireUrl(
        `${REDIRECT}?` +
          new URLSearchParams({ source: 'ping' }).toString(),
      );

      await expect(flow).rejects.toThrow(/missing address\/signature/);
    });
  });
});
