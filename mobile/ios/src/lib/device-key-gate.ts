/**
 * Pure decision logic for the iOS WireGuard *client-key* durability fix
 * (#789, G1 client-side durable fix). Mirrors — 1:1 — the Swift
 * `TunnelControlModule.tunnelManagerDecision` in
 * `modules/TunnelControl/ios/TunnelControl.swift`.
 *
 * ── THE BUG (diagnosed by agent a5a5bcce, proven on-wire 2026-06-14) ──
 *
 * The founder's iOS Network Extension kept retrying an OLD client WG key
 * (`l2bXKoVtjk8dg5tCImHH1qz3LasZr1pkm47I30tuLgE=`, left over from expired
 * Jun-12 sessions) even though the app's NEWEST registered sessions used a
 * DIFFERENT key (`+MOn…`, Jun-13). The app registered a newer key but the NE
 * never adopted it, so the daemon had no peer for the key the NE actually
 * signed with and dropped every handshake ("did not decapsulate against any
 * known peer").
 *
 * ── WHY (the exact mechanism) ──
 *
 * The PacketTunnelProvider extension signs the WireGuard handshake with
 * `clientPrivateKey` read straight out of the `providerConfiguration`
 * *baked into the installed NETunnelProviderManager* (PacketTunnelProvider
 * .swift line 134) — NOT from any live source. And iOS does NOT push an
 * updated `providerConfiguration` into an ALREADY-INSTALLED tunnel: a plain
 * `saveToPreferences` on a REUSED manager silently leaves the OLD baked
 * values in the running NE. So once a manager is installed with key K, the
 * NE keeps signing with K until the manager is fully removed + recreated —
 * regardless of what key the app subsequently registers with vpn-svc.
 *
 * ── THE FIX (this module + its Swift twin) ──
 *
 *   1. SINGLE SOURCE OF TRUTH. `ensureDeviceKeypair` derives the PUBLIC key
 *      it registers with vpn-svc FROM the persisted PRIVATE key, and
 *      `startTunnel` bakes THAT SAME persisted private key. So
 *      registered-pub == pub(baked-priv) by construction (see
 *      `registeredKeyMatchesSigningKey`).
 *   2. RECREATE ON CLIENT-KEY DRIFT. The reuse-vs-recreate gate REUSES the
 *      installed manager ONLY when the key baked into it still equals the
 *      current device key (and the server key + endpoint also match). ANY
 *      drift — a stale baked client key (the `l2bX…` case), a stale server
 *      key, an endpoint change, or a leftover legacy manager — forces a full
 *      teardown + fresh manager so iOS installs the CURRENT key into the NE.
 *      A stale baked client key can therefore never survive a Connect.
 *
 * Like `connection-steps.ts` / `connection-gate.ts` / `grid_balance.ts`,
 * this is intentionally pure (no React, no native modules) so it runs under
 * `testEnvironment: node` without the Expo / react-native runtime — which is
 * the only way to regression-guard this logic, since the native module and
 * NETunnelProviderManager cannot be imported into jest.
 *
 * Refs #789, #762 (server-side recurrence vector), #701 (G1 epic).
 */

/**
 * What `startTunnel` should do with the (possibly already-installed)
 * NETunnelProviderManager on THIS Connect.
 *
 *   - `reuse`    → an installed manager's baked identity already matches the
 *                  desired identity; reuse it untouched (no system prompt, no
 *                  add-config loop).
 *   - `recreate` → drift detected (stale client key / stale server key /
 *                  endpoint change) OR a leftover/legacy manager OR no
 *                  current device key; tear any installed manager DOWN to
 *                  completion and install a FRESH one so iOS bakes the
 *                  current identity into the NE.
 *
 * There is no separate "create" verdict: a clean install (no installed
 * manager) is just `recreate` with nothing to remove first — the Swift side
 * branches on whether a manager exists, but the *decision* is identical
 * ("don't reuse"), which keeps this gate a single boolean.
 */
export type TunnelManagerAction = 'reuse' | 'recreate';

/** The baked vs. desired identity fields the decision is a pure function of. */
export interface TunnelManagerDecisionInput {
  /** Is there an NETunnelProviderManager already installed for this app? */
  hasManager: boolean;
  /**
   * The device's CURRENT persistent WG private key (base64), i.e. the one
   * `startTunnel` is about to bake and whose public half was just registered
   * with vpn-svc. Empty string when none is persisted yet.
   */
  currentClientPriv: string;
  /**
   * The clientPrivateKey baked into the installed manager's
   * providerConfiguration (what the NE is signing with right now). `null`
   * when there's no manager or it carries no client key (e.g. a legacy build).
   */
  bakedClientPriv: string | null;
  /** The peerPublicKey (server identity) baked into the installed manager. */
  bakedPeerPub: string | null;
  /** The peerEndpoint baked into the installed manager. */
  bakedPeerEndpoint: string | null;
  /** The server public key we're about to configure on this Connect. */
  desiredPeerPub: string;
  /** The server endpoint we're about to configure on this Connect. */
  desiredPeerEndpoint: string;
}

/**
 * Decide reuse vs. recreate. REUSE iff ALL of:
 *   - an installed manager exists, AND
 *   - we hold a non-empty current device private key, AND
 *   - the baked clientPrivateKey == that current device key (the #789 guard:
 *     a stale baked client key forces recreate), AND
 *   - the baked peerPublicKey == the desired server key, AND
 *   - the baked peerEndpoint == the desired endpoint.
 * Otherwise RECREATE.
 *
 * This is the EXACT rule encoded in Swift's
 * `TunnelControlModule.tunnelManagerDecision`; the two MUST change together.
 */
export function decideTunnelManagerAction(
  input: TunnelManagerDecisionInput,
): TunnelManagerAction {
  const {
    hasManager,
    currentClientPriv,
    bakedClientPriv,
    bakedPeerPub,
    bakedPeerEndpoint,
    desiredPeerPub,
    desiredPeerEndpoint,
  } = input;

  const canReuse =
    hasManager &&
    currentClientPriv.length > 0 &&
    bakedClientPriv === currentClientPriv &&
    bakedPeerPub === desiredPeerPub &&
    bakedPeerEndpoint === desiredPeerEndpoint;

  return canReuse ? 'reuse' : 'recreate';
}

/**
 * Convenience predicate over the same input: TRUE when the installed manager
 * is safe to reuse. Equivalent to
 * `decideTunnelManagerAction(input) === 'reuse'`; provided so call sites and
 * tests can read either as a boolean or as the action enum.
 */
export function canReuseTunnelManager(
  input: TunnelManagerDecisionInput,
): boolean {
  return decideTunnelManagerAction(input) === 'reuse';
}

/**
 * The single-source-of-truth invariant (#789): the public key the app
 * REGISTERS with vpn-svc must be the public half of the SAME private key the
 * NE will SIGN with. `ensureDeviceKeypair` enforces this natively by
 * deriving the registered public key from the persisted private key and
 * `startTunnel` baking that same private key — so this should ALWAYS hold.
 *
 * This helper makes the invariant checkable from pure code given the
 * registered public key and a `pub(priv)` derivation: callers supply the
 * registered public key, the private key the NE will bake, and a function
 * that derives the base64 WG public key from a base64 private key (in
 * production that's CryptoKit's Curve25519 raw public representation; in
 * tests an injected stub). A mismatch means the registered key and the
 * signing key drifted — exactly the failure this fix eliminates — so a
 * mismatch returning FALSE lets a caller fail loudly rather than start a
 * tunnel doomed to "did not decapsulate against any known peer".
 *
 * @param registeredPublicKey base64 public key handed to vpn-svc.
 * @param signingPrivateKey   base64 private key the NE will bake + sign with.
 * @param derivePublic        base64-priv → base64-pub (Curve25519).
 */
export function registeredKeyMatchesSigningKey(
  registeredPublicKey: string,
  signingPrivateKey: string,
  derivePublic: (privateKeyBase64: string) => string,
): boolean {
  if (!registeredPublicKey || !signingPrivateKey) return false;
  return derivePublic(signingPrivateKey) === registeredPublicKey;
}
