// TunnelControl web stub — RN auto-resolves `*.web.ts` over `index.ts`
// when bundling for web target. The native module isn't available on
// web (NETunnelProviderManager is iOS-only). This stub provides the
// same shape so the toggle screen renders + Playwright can walk the
// UI surface for #568 UAT without hitting the requireNativeModule
// "Cannot find native module 'TunnelControl'" error.

export type TunnelStatus =
  | 'invalid'
  | 'disconnected'
  | 'connecting'
  | 'connected'
  | 'reasserting'
  | 'disconnecting'
  | 'unknown';

export interface TunnelConfig {
  peerPublicKey: string;
  peerEndpoint: string;
  customerInnerCIDR: string;
  allowedIPs: string;
  region: string;
  sessionId: string;
}

interface Subscription {
  remove: () => void;
}

const listeners = new Set<(e: { status: TunnelStatus }) => void>();
let webStatus: TunnelStatus = 'disconnected';

function emit(status: TunnelStatus) {
  webStatus = status;
  for (const l of listeners) l({ status });
}

export const TunnelControl = {
  getStatus: async (): Promise<TunnelStatus> => webStatus,

  startTunnel: async (_config: TunnelConfig): Promise<void> => {
    // Web stub: simulate the connecting → disconnected cycle that the
    // real iOS path would do when WireGuardKit isn't linked (#576).
    // Lets the toggle UI exercise its state machine on web without
    // pretending to actually establish a tunnel.
    emit('connecting');
    setTimeout(() => emit('disconnected'), 1500);
  },

  stopTunnel: async (): Promise<void> => {
    emit('disconnected');
  },

  sendProviderMessage: async (_command: string): Promise<string | null> => null,

  onStatusChange: (listener: (e: { status: TunnelStatus }) => void): Subscription => {
    listeners.add(listener);
    return { remove: () => listeners.delete(listener) };
  },
};

export default TunnelControl;
