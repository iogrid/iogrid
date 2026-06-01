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
  /** Comma-separated CIDR list, e.g. "0.0.0.0/0". */
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

interface TunnelControlNative {
  getStatus(): Promise<TunnelStatus>;
  startTunnel(config: TunnelConfig): Promise<void>;
  stopTunnel(): Promise<void>;
  sendProviderMessage(command: string): Promise<string | null>;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type TunnelEvents = Record<string, (...args: any[]) => void> & {
  status: (e: { status: TunnelStatus }) => void;
};

const native = requireNativeModule<TunnelControlNative>('TunnelControl');
const emitter = new EventEmitter<TunnelEvents>(native as never);

export const TunnelControl = {
  /** Current tunnel status (one HTTP-style round trip to the OS). */
  getStatus: (): Promise<TunnelStatus> => native.getStatus(),

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
};

export default TunnelControl;
