// Tests for the iOS WireGuard client-key durability gate (Refs #789, #701).
//
// These pin the EXACT reuse-vs-recreate rule that Swift's
// `TunnelControlModule.tunnelManagerDecision` encodes (the two copies must
// stay in lockstep). The G1 bug they guard against: the iOS Network
// Extension signs with the `clientPrivateKey` baked into the INSTALLED
// NETunnelProviderManager, and iOS won't push an updated providerConfiguration
// into an already-installed tunnel — so a STALE baked client key (the
// founder's `l2bX…` left over from an expired session) keeps signing every
// handshake even after the app registers a newer key, and the daemon drops it
// ("did not decapsulate against any known peer").
//
// The fix forces a teardown + recreate whenever the baked client key drifts
// from the current device key, so the stale key can never survive a Connect;
// and it guarantees the registered public key == pub(signing private key) by
// construction. The cases below lock down both halves.
//
// Pure module → no mocks needed; it imports zero native code.

import {
  canReuseTunnelManager,
  decideTunnelManagerAction,
  registeredKeyMatchesSigningKey,
  type TunnelManagerDecisionInput,
} from '../device-key-gate';

// Realistic base64 WG keys from the actual #789 incident, so the test data
// reads like the bug it guards.
const STALE_CLIENT_PRIV = 'kCYFw4kQ1h0p9hQ7gQ2rM3sT5uV7wX9yA1bC3dE5f8='; // pub == l2bX…
const STALE_CLIENT_PUB = 'l2bXKoVtjk8dg5tCImHH1qz3LasZr1pkm47I30tuLgE='; // the retried-forever key
const CURRENT_CLIENT_PRIV = 'aB2cD4eF6gH8iJ0kL2mN4oP6qR8sT0uV2wX4yZ6A8c='; // pub == +MOn…
const CURRENT_CLIENT_PUB = '+MOnQ1w2e3r4t5y6u7i8o9p0aSdFgHjKlZxCvBnM1q='; // newest registered key

const SERVER_PUB = 'cM9MQfzK6sPlGqaW4dFh2j3k4l5m6n7o8p9q0rStUvGzs=';
const ENDPOINT = '188.135.27.125:51820';

/** A fully-matching steady-state input (every baked field == desired). */
function steadyState(): TunnelManagerDecisionInput {
  return {
    hasManager: true,
    currentClientPriv: CURRENT_CLIENT_PRIV,
    bakedClientPriv: CURRENT_CLIENT_PRIV,
    bakedPeerPub: SERVER_PUB,
    bakedPeerEndpoint: ENDPOINT,
    desiredPeerPub: SERVER_PUB,
    desiredPeerEndpoint: ENDPOINT,
  };
}

describe('decideTunnelManagerAction — reuse-vs-recreate gate (#789)', () => {
  // ── The core #789 case ──────────────────────────────────────────────
  it('RECREATEs when the installed manager baked a STALE client key (the l2bX symptom)', () => {
    // The NE still has the old `l2bX…` private baked; the device has rotated
    // to the `+MOn…` key the app just registered. The gate MUST tear it down.
    const input: TunnelManagerDecisionInput = {
      ...steadyState(),
      bakedClientPriv: STALE_CLIENT_PRIV, // what the NE keeps signing with
      currentClientPriv: CURRENT_CLIENT_PRIV, // what we just registered
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
    expect(canReuseTunnelManager(input)).toBe(false);
  });

  it('REUSEs when every baked identity field already matches (steady state)', () => {
    const input = steadyState();
    expect(decideTunnelManagerAction(input)).toBe('reuse');
    expect(canReuseTunnelManager(input)).toBe(true);
  });

  // ── Other drift dimensions all force recreate ───────────────────────
  it('RECREATEs on STALE SERVER key drift (the on-wire #760 finding)', () => {
    const input: TunnelManagerDecisionInput = {
      ...steadyState(),
      bakedPeerPub: 'STALEserverKEYfromPreJun10deploymentAAAAAAAAA=',
      desiredPeerPub: SERVER_PUB,
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  it('RECREATEs on endpoint drift (provider IP/port changed)', () => {
    const input: TunnelManagerDecisionInput = {
      ...steadyState(),
      bakedPeerEndpoint: '10.0.0.9:51820',
      desiredPeerEndpoint: ENDPOINT,
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  it('RECREATEs when there is NO installed manager (clean install / first connect)', () => {
    const input: TunnelManagerDecisionInput = {
      ...steadyState(),
      hasManager: false,
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  it('RECREATEs when a leftover/legacy manager baked NO client key at all (null)', () => {
    // An older build's manager with no clientPrivateKey in its config: null
    // !== any non-empty current key, so we must recreate rather than reuse a
    // manager whose NE would fall back to a Keychain/empty key.
    const input: TunnelManagerDecisionInput = {
      ...steadyState(),
      bakedClientPriv: null,
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  it('RECREATEs when the device key is missing (currentClientPriv empty) — never reuse keyless', () => {
    const input: TunnelManagerDecisionInput = {
      ...steadyState(),
      currentClientPriv: '',
      bakedClientPriv: '', // even if baked also empty, an empty current key can't be reused
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  it('does NOT treat empty-string baked == empty-string current as reusable', () => {
    // Guards the `currentClientPriv.length > 0` clause: two empties must not
    // collapse into a spurious reuse (that would bake an empty key into a
    // tunnel that the extension rejects as missing).
    const input: TunnelManagerDecisionInput = {
      hasManager: true,
      currentClientPriv: '',
      bakedClientPriv: '',
      bakedPeerPub: SERVER_PUB,
      bakedPeerEndpoint: ENDPOINT,
      desiredPeerPub: SERVER_PUB,
      desiredPeerEndpoint: ENDPOINT,
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  it('combined drift (stale client AND server AND endpoint) → recreate', () => {
    const input: TunnelManagerDecisionInput = {
      hasManager: true,
      currentClientPriv: CURRENT_CLIENT_PRIV,
      bakedClientPriv: STALE_CLIENT_PRIV,
      bakedPeerPub: 'old',
      bakedPeerEndpoint: 'old:1',
      desiredPeerPub: SERVER_PUB,
      desiredPeerEndpoint: ENDPOINT,
    };
    expect(decideTunnelManagerAction(input)).toBe('recreate');
  });

  // ── Fresh-session-each-connect property ─────────────────────────────
  // Whatever NEW server key + endpoint a freshly-minted session returns,
  // if it differs from what the NE baked, the gate recreates — so the NE is
  // never left signing/encrypting with a previous session's stale identity.
  it('a fresh session whose peer config differs from the baked one always recreates', () => {
    const freshSessions = [
      { peerPub: 'freshKeyA===', endpoint: '1.1.1.1:51820' },
      { peerPub: 'freshKeyB===', endpoint: '2.2.2.2:51820' },
      { peerPub: SERVER_PUB, endpoint: '3.3.3.3:51820' }, // same key, new endpoint
    ];
    for (const s of freshSessions) {
      const input: TunnelManagerDecisionInput = {
        hasManager: true,
        currentClientPriv: CURRENT_CLIENT_PRIV,
        bakedClientPriv: CURRENT_CLIENT_PRIV, // client key fine…
        bakedPeerPub: SERVER_PUB, // …but baked server identity is the OLD session's
        bakedPeerEndpoint: ENDPOINT,
        desiredPeerPub: s.peerPub,
        desiredPeerEndpoint: s.endpoint,
      };
      expect(decideTunnelManagerAction(input)).toBe('recreate');
    }
  });
});

describe('registeredKeyMatchesSigningKey — single-source-of-truth invariant (#789)', () => {
  // The production derivation is CryptoKit Curve25519 raw-public; in tests we
  // inject a deterministic stub keyed off our known priv→pub pairs so the
  // assertion exercises the comparison logic, not a real curve.
  const derive = (priv: string): string => {
    const table: Record<string, string> = {
      [CURRENT_CLIENT_PRIV]: CURRENT_CLIENT_PUB,
      [STALE_CLIENT_PRIV]: STALE_CLIENT_PUB,
    };
    return table[priv] ?? `pub(${priv})`;
  };

  it('TRUE when the registered public key is the public half of the signing private key', () => {
    // The whole point of the fix: ensureDeviceKeypair registered pub(+MOn…)
    // and startTunnel bakes the matching +MOn… private → they agree.
    expect(
      registeredKeyMatchesSigningKey(CURRENT_CLIENT_PUB, CURRENT_CLIENT_PRIV, derive),
    ).toBe(true);
  });

  it('FALSE when the app registered a NEW pub but the NE would sign with the STALE priv (the bug)', () => {
    // Registered the +MOn… pub, but the manager still bakes the l2bX… priv:
    // pub(l2bX-priv) == l2bX-pub != +MOn-pub → mismatch caught.
    expect(
      registeredKeyMatchesSigningKey(CURRENT_CLIENT_PUB, STALE_CLIENT_PRIV, derive),
    ).toBe(false);
  });

  it('FALSE on empty inputs (no key registered / no signing key yet)', () => {
    expect(registeredKeyMatchesSigningKey('', CURRENT_CLIENT_PRIV, derive)).toBe(false);
    expect(registeredKeyMatchesSigningKey(CURRENT_CLIENT_PUB, '', derive)).toBe(false);
  });

  it('round-trips: deriving pub from the signing priv and registering THAT always matches', () => {
    // Models the post-fix flow: register exactly pub(priv) → invariant holds
    // for any priv, which is the construction guarantee the native code makes.
    for (const priv of [CURRENT_CLIENT_PRIV, STALE_CLIENT_PRIV, 'arbitraryPrivKey===']) {
      const registered = derive(priv);
      expect(registeredKeyMatchesSigningKey(registered, priv, derive)).toBe(true);
    }
  });
});
