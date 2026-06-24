// TunnelControl — TypeScript wrapper around the native module.
//
// JS-side consumers shouldn't have to touch the raw Expo Modules
// `requireNativeModule` boilerplate — this file is the public face.
// Pairs with `ios/TunnelControl.swift`.

import { requireNativeModule, EventEmitter, type EventSubscription } from 'expo-modules-core';

/** Mirrors the Swift TunnelConfig record. */
export interface TunnelConfig {
  peerPublicKey: string;
  peerEndpoint: string;
  customerInnerCIDR: string;
  /** Comma-separated CIDR list. Full-tunnel is "0.0.0.0/0,::/0" so both
   *  IPv4 and IPv6 default routes enter the tunnel (#701). */
  allowedIPs: string;
  region: string;
  sessionId: string;
}

export type TunnelStatus =
  | 'invalid'
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reasserting'
  | 'disconnecting'
  | 'unknown';

/**
 * Per-tick stats from the PacketTunnelProvider extension (#587).
 *
 * Wire shape mirrors `Stats.swift` in the extension. `sessionId` is
 * the iogrid session id (UUIDv7 from vpn-svc), NOT the OS-level VPN
 * connection UUID — the latter is opaque + only useful for OS log
 * correlation.
 *
 * `handshakeAge === -1` means the WG handshake hasn't completed yet
 * (tunnel just established). `latency === -1` means no probe sample
 * is available yet (Track 4 / #591 will fill this from the path-probe
 * RTT once that loop is wired).
 */
export interface TunnelStats {
  /** iogrid session id (UUIDv7) */
  sessionId: string;
  /** bytes sent over the tunnel since startTunnel */
  sent: number;
  /** bytes received over the tunnel since startTunnel */
  received: number;
  /** path latency in ms (-1 if no probe sample yet) */
  latency: number;
  /** seconds since the last successful WG handshake (-1 if no handshake yet) */
  handshakeAge: number;
  /** wall-clock timestamp when the extension captured these counters */
  capturedAtUnixMs: number;
}

interface TunnelControlNative {
  getStatus(): Promise<TunnelStatus>;
  ensureDeviceKeypair(): Promise<string>;
  startTunnel(config: TunnelConfig): Promise<void>;
  stopTunnel(): Promise<void>;
  sendProviderMessage(command: string): Promise<string | null>;
}

// The native event payload uses snake_case (matches the Swift Stats
// CodingKeys); JS consumers get the camelCase TunnelStats above via
// the `onStatsUpdate` mapper below.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
interface NativeStatsPayload {
  session_id: string;
  bytes_sent: number;
  bytes_received: number;
  path_latency_ms: number;
  handshake_age_seconds: number;
  captured_at_unix_ms: number;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type TunnelEvents = Record<string, (...args: any[]) => void> & {
  status: (e: { status: TunnelStatus }) => void;
  stats: (e: NativeStatsPayload) => void;
};

const native = requireNativeModule<TunnelControlNative>('TunnelControl');
const emitter = new EventEmitter<TunnelEvents>(native as never);

export const TunnelControl = {
  /** Current tunnel status (one HTTP-style round trip to the OS). */
  getStatus: (): Promise<TunnelStatus> => native.getStatus(),

  /**
   * Ensure the device's persistent WireGuard keypair exists and return
   * its base64 PUBLIC key. Generated once per install and shared with the
   * tunnel extension via the App Group; the private key never crosses the
   * JS bridge. Call this BEFORE requesting a mobile session so the app can
   * register the real device public key with vpn-svc (#701).
   */
  ensureDeviceKeypair: (): Promise<string> => native.ensureDeviceKeypair(),

  /**
   * Save the VPN configuration + start the tunnel. The first call on
   * a device triggers iOS's "iogrid would like to add VPN
   * Configurations" sheet — the user must tap Allow + authenticate.
   * Subsequent calls reuse the saved configuration.
   */
  startTunnel: (config: TunnelConfig): Promise<void> => native.startTunnel(config),

  /** Stop the tunnel (does NOT delete the saved configuration). */
  stopTunnel: (): Promise<void> => native.stopTunnel(),

  /** Send a JSON command to the PacketTunnelProvider extension via NETunnelProviderSession.sendProviderMessage. Used by the roaming flow (#572) to ask the extension to re-probe peers. */
  sendProviderMessage: (command: string): Promise<string | null> =>
    native.sendProviderMessage(command),

  /** Subscribe to status updates emitted by the OS on NEVPNStatusDidChange. */
  onStatusChange: (listener: (e: { status: TunnelStatus }) => void): EventSubscription =>
    emitter.addListener('status', listener),

  /**
   * Subscribe to per-tick stats from the PacketTunnelProvider extension (#587).
   *
   * Fires roughly every 1s while a tunnel is up. Stats are captured by the
   * extension, written to App Group UserDefaults, and polled+forwarded by
   * the main app's TunnelControl module — so the cadence is approximate
   * and may skip ticks under heavy main-thread load.
   *
   * The native event uses snake_case; this wrapper maps to camelCase
   * TunnelStats for JS ergonomics.
   */
  onStatsUpdate: (listener: (stats: TunnelStats) => void): EventSubscription =>
    emitter.addListener('stats', (e: NativeStatsPayload) => {
      listener({
        sessionId: e.session_id,
        sent: e.bytes_sent,
        received: e.bytes_received,
        latency: e.path_latency_ms,
        handshakeAge: e.handshake_age_seconds,
        capturedAtUnixMs: e.captured_at_unix_ms,
      });
    }),
};

export default TunnelControl;
