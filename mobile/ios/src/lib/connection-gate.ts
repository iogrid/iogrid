/**
 * Pure gating logic for the user-facing "Connected" state (#701).
 *
 * THE BUG this guards against: iOS reports `NEVPNStatus.connected` the
 * instant the PacketTunnelProvider's `startTunnel` completion handler
 * returns — which happens as soon as `WireGuardAdapter.start` brings the
 * tunnel *interface* up. That is BEFORE any WireGuard handshake has
 * completed. A black-hole tunnel (the peer endpoint is wrong / the server
 * never answers the handshake init — exactly the #701 "did not decapsulate
 * against any known peer" failure mode) therefore sits in OS-`connected`
 * forever while ZERO traffic flows. The old screen mapped OS-`connected`
 * straight to the green "Connected / Protected" affordance, so the user
 * believed they were protected when nothing was encrypted or routed.
 *
 * THE FIX: OS-`connected` is treated as NECESSARY-BUT-NOT-SUFFICIENT. We
 * only promote to the user-facing CONNECTED state once there is EVIDENCE
 * of a real handshake. The evidence reuses signals the PacketTunnelProvider
 * stats loop (#587, Stats.swift) already emits every ~1s via
 * `TunnelControl.onStatsUpdate`, so no new native IPC is invented:
 *
 *   - `handshakeAge >= 0` within a sane freshness window — parsed from
 *     WireGuard's `last_handshake_time_sec` (0 => never handshaked => the
 *     JS layer surfaces it as -1). A non-negative, recent value is the
 *     single most authoritative proof the Noise-IK handshake completed.
 *   - `received > 0` — the peer sent at least one byte back down the
 *     tunnel; impossible without a completed handshake.
 *   - `latency >= 0` — the existing path-probe (#591) returned a real RTT
 *     sample (-1 means "no probe sample yet"). A real sample means a
 *     packet round-tripped, which also requires a live peer.
 *
 * Any ONE of those is sufficient. Until at least one appears we hold the
 * tunnel in `VERIFYING` (rendered as the in-progress affordance, never the
 * green "Protected" one). If none appears within `HANDSHAKE_TIMEOUT_MS`
 * while the OS still claims connected, we declare the connect FAILED
 * (black-hole) so the user gets an honest non-connected state instead of a
 * fake green shield.
 *
 * This module is intentionally pure (no React, no native modules — the
 * type-only import is erased by the compiler), mirroring
 * `connection-steps.ts` / `grid_balance.ts` so it is unit-testable under
 * `testEnvironment: node` without loading the Expo / react-native runtime.
 *
 * Refs #701. Pairs with src/app/index.tsx + modules/TunnelControl.
 */

import type { TunnelStats } from 'iogrid-tunnel-control';

/**
 * The user-facing tunnel lifecycle. `VERIFYING` is the #701 addition: the
 * intermediate state between "OS says the NE is up" and "a real handshake
 * is confirmed". The screen renders it with the same in-progress affordance
 * as CONNECTING so the full "Connected / Protected" UI is NEVER shown on a
 * black-hole tunnel.
 */
export type TunnelState =
  | 'OFF'
  | 'CONNECTING'
  | 'VERIFYING'
  | 'CONNECTED'
  | 'DISCONNECTING';

/**
 * Max time we wait for handshake evidence after the OS reports connected
 * before declaring the tunnel a black-hole and failing the attempt.
 *
 * A real WireGuard handshake completes in well under a second on a healthy
 * path; the stats loop ticks every ~1s. 10s spans ~10 stat ticks plus
 * generous margin for a slow first handshake / a roaming re-pin, while
 * still surfacing a genuinely dead tunnel fast enough that the user isn't
 * left staring at a fake "connecting" for a minute.
 */
export const HANDSHAKE_TIMEOUT_MS = 10_000;

/**
 * Upper bound on `handshakeAge` (seconds) for it to count as evidence of a
 * LIVE peer. WireGuard rekeys every ~120s, so a handshake older than a few
 * minutes can mean the peer has since gone away. We accept up to 180s — a
 * comfortable margin above the rekey interval — so a brief stats gap or a
 * just-past-rekey tick still reads as connected, while a long-stale value
 * (e.g. a counter left over from a previous session) does not.
 */
export const MAX_HANDSHAKE_AGE_SECONDS = 180;

/**
 * True when the stats snapshot proves a real WireGuard handshake / live
 * peer. Any one signal is sufficient; all are derived from counters the
 * PacketTunnelProvider already emits (Stats.swift). `null`/`undefined`
 * stats (no tick yet) is, by definition, no evidence.
 *
 * Defensive against the JS sentinel convention: `handshakeAge === -1`,
 * `latency === -1`, and `received === 0` all mean "not yet" — only
 * strictly-positive / non-negative real values count.
 */
export function hasHandshakeEvidence(stats: TunnelStats | null | undefined): boolean {
  if (!stats) return false;

  // 1. A real, recent WG handshake — the most authoritative proof.
  if (
    typeof stats.handshakeAge === 'number' &&
    stats.handshakeAge >= 0 &&
    stats.handshakeAge <= MAX_HANDSHAKE_AGE_SECONDS
  ) {
    return true;
  }

  // 2. Bytes came back DOWN the tunnel — impossible without a handshake.
  if (typeof stats.received === 'number' && stats.received > 0) {
    return true;
  }

  // 3. The existing path probe returned a real RTT sample (-1 == none yet).
  if (typeof stats.latency === 'number' && stats.latency >= 0) {
    return true;
  }

  return false;
}

/** Maps the raw native TunnelControl status string to a coarse phase. */
export type NativeTunnelStatus =
  | 'invalid'
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reasserting'
  | 'disconnecting'
  | 'unknown';

/**
 * Inputs to the gate decision. Kept as a plain record so the whole
 * decision is a pure function of explicit values (trivially testable).
 */
export interface GateInput {
  /** Latest raw OS status from NEVPNStatusDidChange / TunnelControl. */
  nativeStatus: NativeTunnelStatus;
  /** Latest stats tick (or null if none received yet this session). */
  latestStats: TunnelStats | null;
  /**
   * ms since the OS first reported `connected` for the CURRENT bring-up
   * (0/undefined if it hasn't reported connected yet). Used only to decide
   * the black-hole timeout; the caller owns the clock.
   */
  msSinceNativeConnected?: number;
}

/**
 * The gate's verdict for the user-facing state, given the raw OS status +
 * the latest evidence. The screen owns the OFF/DISCONNECTING transitions
 * it drives directly (tap-to-disconnect); this function only adjudicates
 * the contested "is the tunnel REALLY connected" question.
 *
 *   - OS not connected           → mirror it (connecting / off).
 *   - OS connected + evidence     → CONNECTED (the happy path — unchanged
 *                                   behaviour once a real handshake lands).
 *   - OS connected, no evidence,
 *     within timeout              → VERIFYING (hold; never show "Protected").
 *   - OS connected, no evidence,
 *     past timeout                → FAILED (black-hole — honest non-connect).
 *
 * Returns a discriminated result so the caller can both set the visual
 * state AND know when to tear the dead tunnel down + alert.
 */
export type GateVerdict =
  | { state: 'OFF' }
  | { state: 'CONNECTING' }
  | { state: 'VERIFYING' }
  | { state: 'CONNECTED' }
  | { state: 'DISCONNECTING' }
  | { state: 'FAILED'; reason: 'handshake-timeout' };

export function evaluateGate(input: GateInput): GateVerdict {
  const { nativeStatus, latestStats, msSinceNativeConnected } = input;

  switch (nativeStatus) {
    case 'connecting':
    case 'reasserting':
      return { state: 'CONNECTING' };
    case 'disconnecting':
      return { state: 'DISCONNECTING' };
    case 'disconnected':
    case 'invalid':
    case 'unknown':
      return { state: 'OFF' };
    case 'connected':
      // The crux of #701: OS-connected is necessary, not sufficient.
      if (hasHandshakeEvidence(latestStats)) {
        return { state: 'CONNECTED' };
      }
      if ((msSinceNativeConnected ?? 0) >= HANDSHAKE_TIMEOUT_MS) {
        // No handshake within the window while the OS still claims up:
        // this is a black-hole tunnel. Fail honestly.
        return { state: 'FAILED', reason: 'handshake-timeout' };
      }
      // Up but unproven — hold in the intermediate state.
      return { state: 'VERIFYING' };
    default:
      return { state: 'OFF' };
  }
}
