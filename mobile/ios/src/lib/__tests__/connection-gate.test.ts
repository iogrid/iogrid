// Tests for the user-facing "Connected" handshake gate (Refs #701).
//
// THE REGRESSION THIS GUARDS: iOS reports NEVPNStatus.connected the instant
// the tunnel INTERFACE comes up — before any WireGuard handshake. A
// black-hole tunnel (dead/wrong peer) therefore reads as OS-`connected`
// while ZERO traffic flows. The old screen showed the green
// "Connected / Protected" affordance on that bare OS status, so the user
// believed they were protected when nothing was encrypted or routed. The
// gate below makes OS-`connected` necessary-but-not-sufficient: the
// user-facing CONNECTED state is reached ONLY with evidence of a real
// handshake (recent handshakeAge, received bytes, or a real latency probe
// sample). A regression here silently reinstates the fake-connection bug —
// green CI, furious user on a black-hole tunnel.
//
// Pure logic (no React, no native modules — the TunnelStats import is
// type-only), driven directly the same way connection-steps / grid_balance
// tests stay out of the react-native / expo runtime.
//
// Matrix:
//   hasHandshakeEvidence
//     1. null / undefined stats          → false (no tick yet)
//     2. fresh handshakeAge (0)          → true
//     3. recent handshakeAge (e.g. 30s)  → true
//     4. handshakeAge === -1 (sentinel)  → false (WG never handshaked)
//     5. stale handshakeAge (> max)      → false (peer may be gone)
//     6. received > 0                    → true (bytes came back)
//     7. latency >= 0 (real probe)       → true
//     8. latency === -1, received 0,
//        handshakeAge -1                 → false (all sentinels)
//   evaluateGate
//     9. native connecting / reasserting → CONNECTING
//    10. native disconnecting            → DISCONNECTING
//    11. native disconnected/invalid/unknown → OFF
//    12. connected + evidence            → CONNECTED  (happy path intact)
//    13. connected, no evidence, t<timeout → VERIFYING (never "connected")
//    14. connected, no evidence, t>=timeout → FAILED (black-hole)
//    15. connected, evidence, even past timeout → CONNECTED (evidence wins)

import type { TunnelStats } from 'iogrid-tunnel-control';
import {
  evaluateGate,
  hasHandshakeEvidence,
  HANDSHAKE_TIMEOUT_MS,
  MAX_HANDSHAKE_AGE_SECONDS,
  type GateInput,
  type NativeTunnelStatus,
} from '@/lib/connection-gate';

// Build a stats snapshot with the JS sentinel defaults ("nothing yet"),
// overriding only the field a given test exercises. Mirrors the camelCase
// TunnelStats the TunnelControl wrapper emits from the extension's tick.
const stats = (over: Partial<TunnelStats> = {}): TunnelStats => ({
  sessionId: 'sess-1',
  sent: 0,
  received: 0,
  latency: -1,
  handshakeAge: -1,
  capturedAtUnixMs: 1_700_000_000_000,
  ...over,
});

describe('hasHandshakeEvidence', () => {
  it('is false with no stats tick yet (null / undefined)', () => {
    expect(hasHandshakeEvidence(null)).toBe(false);
    expect(hasHandshakeEvidence(undefined)).toBe(false);
  });

  it('is true on a fresh handshake (age 0)', () => {
    expect(hasHandshakeEvidence(stats({ handshakeAge: 0 }))).toBe(true);
  });

  it('is true on a recent handshake (age 30s)', () => {
    expect(hasHandshakeEvidence(stats({ handshakeAge: 30 }))).toBe(true);
  });

  it('is false when handshakeAge is the -1 sentinel (WG never handshaked)', () => {
    // The exact black-hole signature: tunnel up, no handshake yet.
    expect(hasHandshakeEvidence(stats({ handshakeAge: -1 }))).toBe(false);
  });

  it('is false when the handshake is too stale to prove a live peer', () => {
    expect(
      hasHandshakeEvidence(stats({ handshakeAge: MAX_HANDSHAKE_AGE_SECONDS + 1 })),
    ).toBe(false);
  });

  it('accepts a handshake exactly at the freshness boundary', () => {
    expect(
      hasHandshakeEvidence(stats({ handshakeAge: MAX_HANDSHAKE_AGE_SECONDS })),
    ).toBe(true);
  });

  it('is true when bytes came back down the tunnel (received > 0)', () => {
    // Receiving is impossible without a completed handshake.
    expect(hasHandshakeEvidence(stats({ received: 1 }))).toBe(true);
  });

  it('is true when the path probe returned a real RTT sample (latency >= 0)', () => {
    expect(hasHandshakeEvidence(stats({ latency: 0 }))).toBe(true);
    expect(hasHandshakeEvidence(stats({ latency: 42 }))).toBe(true);
  });

  it('is false when every signal is still its "not yet" sentinel', () => {
    // tunnel interface up, but no handshake / no bytes / no probe — the
    // canonical black-hole tunnel. Must NOT read as evidence.
    expect(
      hasHandshakeEvidence(stats({ handshakeAge: -1, received: 0, latency: -1 })),
    ).toBe(false);
  });

  it('ignores sent-only traffic (we can spray packets at a dead peer)', () => {
    // Sending bytes proves nothing — the OS will happily queue packets
    // into a black-hole tunnel. Only RECEIVED bytes prove a live peer.
    expect(hasHandshakeEvidence(stats({ sent: 9999, received: 0 }))).toBe(false);
  });
});

describe('evaluateGate', () => {
  const input = (over: Partial<GateInput>): GateInput => ({
    nativeStatus: 'disconnected',
    latestStats: null,
    msSinceNativeConnected: 0,
    ...over,
  });

  it('maps native connecting / reasserting → CONNECTING', () => {
    expect(evaluateGate(input({ nativeStatus: 'connecting' }))).toEqual({
      state: 'CONNECTING',
    });
    expect(evaluateGate(input({ nativeStatus: 'reasserting' }))).toEqual({
      state: 'CONNECTING',
    });
  });

  it('maps native disconnecting → DISCONNECTING', () => {
    expect(evaluateGate(input({ nativeStatus: 'disconnecting' }))).toEqual({
      state: 'DISCONNECTING',
    });
  });

  it('maps native disconnected / invalid / unknown → OFF', () => {
    for (const s of ['disconnected', 'invalid', 'unknown'] as NativeTunnelStatus[]) {
      expect(evaluateGate(input({ nativeStatus: s }))).toEqual({ state: 'OFF' });
    }
  });

  it('HAPPY PATH: connected + real handshake → CONNECTED', () => {
    expect(
      evaluateGate(
        input({
          nativeStatus: 'connected',
          latestStats: stats({ handshakeAge: 0 }),
          msSinceNativeConnected: 1200,
        }),
      ),
    ).toEqual({ state: 'CONNECTED' });
  });

  it('connected but NO handshake within the window → VERIFYING (never connected)', () => {
    const verdict = evaluateGate(
      input({
        nativeStatus: 'connected',
        latestStats: stats({ handshakeAge: -1, received: 0, latency: -1 }),
        msSinceNativeConnected: HANDSHAKE_TIMEOUT_MS - 1,
      }),
    );
    expect(verdict).toEqual({ state: 'VERIFYING' });
    // The whole point of #701: this must NOT be CONNECTED.
    expect(verdict.state).not.toBe('CONNECTED');
  });

  it('connected with NO stats tick at all → VERIFYING (not connected)', () => {
    expect(
      evaluateGate(
        input({
          nativeStatus: 'connected',
          latestStats: null,
          msSinceNativeConnected: 500,
        }),
      ),
    ).toEqual({ state: 'VERIFYING' });
  });

  it('connected, no handshake, PAST the timeout → FAILED (black-hole)', () => {
    expect(
      evaluateGate(
        input({
          nativeStatus: 'connected',
          latestStats: stats({ handshakeAge: -1, received: 0, latency: -1 }),
          msSinceNativeConnected: HANDSHAKE_TIMEOUT_MS,
        }),
      ),
    ).toEqual({ state: 'FAILED', reason: 'handshake-timeout' });
  });

  it('evidence wins even past the timeout (late but real handshake → CONNECTED)', () => {
    // A slow-but-real handshake that lands just after the timeout window
    // must still resolve to CONNECTED, never FAILED — evidence trumps the
    // clock. (Guards the happy path from a flaky/slow first handshake.)
    expect(
      evaluateGate(
        input({
          nativeStatus: 'connected',
          latestStats: stats({ received: 64 }),
          msSinceNativeConnected: HANDSHAKE_TIMEOUT_MS + 5_000,
        }),
      ),
    ).toEqual({ state: 'CONNECTED' });
  });
});
