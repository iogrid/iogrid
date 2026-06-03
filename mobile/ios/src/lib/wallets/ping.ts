// Ping Wallet adapter — ⚠️ DEPRECATED / DO NOT EXTEND (#629).
//
// @deprecated The `ping://wallet/connect` "external wallet connect" protocol
// below was a SELF-INVENTED contract — Ping never published or agreed to it
// (Ping's own app uses `cash://` internally). Ping's PUBLISHED integration
// contract (ping-cash:docs/coordination/iogrid-ping-integration.md, ADR 0028/
// 0029) is NOT a "connect a wallet" model at all: it is a per-payment
// **Universal-Link handoff** — iogrid opens `https://ping.cash/approve?…`,
// Ping's Privy-MPC wallet signs an SPL Approve (delegate), and bounces back to
// `iogrid://vpn/activated?ok=1&signature=<sig>`. That canonical flow lives in
// `./ping-pay.ts` (buildVpnApproveUrl + parseVpnReturn + verifyApprovalBestEffort).
//
// There is therefore no "Ping wallet" to connect — Ping is the payment rail,
// invoked per transaction via Universal Link, not a wallet bound to the iogrid
// account. This adapter remains only so the existing wallet-registry / connect-
// wallet UI keeps compiling; it should be REMOVED once the connect-wallet screen
// is realigned to drop the Ping-connect option (Phantom stays — it's a real
// connectable wallet for $GRID balance / SIWS). Tracked on #629.
//
// The legacy request/response shape (kept for reference until removal):
//   Request:  ping://wallet/connect?app=iogrid&redirect=iogrid://wallet-callback&challenge=<…>
//   Response: iogrid://wallet-callback?source=ping&address=<pubkey>&signature=<sig>

import * as Linking from 'expo-linking';

import type { BindChallenge, Wallet, WalletConnectResult } from './types';

const PING_CONNECT_URL = 'ping://wallet/connect';
const REDIRECT_LINK = 'iogrid://wallet-callback';

type Pending = {
  resolve: (r: WalletConnectResult) => void;
  reject: (e: Error) => void;
  challenge: BindChallenge;
};

let pending: Pending | null = null;
let linkingSub: { remove(): void } | null = null;

function ensureLinkingSubscription() {
  if (linkingSub) return;
  linkingSub = Linking.addEventListener('url', (event) => {
    try {
      handlePingCallback(event.url);
    } catch (err) {
      if (pending) {
        const p = pending;
        pending = null;
        p.reject(err instanceof Error ? err : new Error(String(err)));
      }
    }
  });
}

function handlePingCallback(url: string) {
  if (!url.startsWith(REDIRECT_LINK)) return;
  if (!pending) return;
  const parsed = Linking.parse(url);
  const params = (parsed.queryParams ?? {}) as Record<string, string>;
  // We share `iogrid://wallet-callback` across Phantom + Ping. The
  // `source` discriminator tells us which adapter should consume the
  // callback. Phantom never sets `source=ping`, so a ping-flow
  // listener bailing on Phantom callbacks works in both directions.
  if (params.source !== 'ping') return;

  if (params.error) {
    const p = pending;
    pending = null;
    p.reject(new Error(`ping: ${params.error}`));
    return;
  }
  const { address, signature } = params;
  if (!address || !signature) {
    const p = pending;
    pending = null;
    p.reject(new Error('ping: callback missing address/signature'));
    return;
  }
  const result: WalletConnectResult = {
    address,
    provider: 'ping',
    challenge: pending.challenge,
    signatureBase58: signature,
  };
  const p = pending;
  pending = null;
  p.resolve(result);
}

class PingWallet implements Wallet {
  readonly provider = 'ping' as const;

  async isInstalled(): Promise<boolean> {
    try {
      return await Linking.canOpenURL('ping://');
    } catch {
      return false;
    }
  }

  appStoreURL(): string {
    // ping cash — the openova-group consumer payments app. Update when
    // a stable App Store ID lands (build still rolling under TestFlight
    // at time of writing).
    return 'https://apps.apple.com/app/ping-cash/id6747000000';
  }

  async connectAndSign(challenge: BindChallenge): Promise<WalletConnectResult> {
    if (pending) {
      throw new Error('ping: another connect flow is already in progress');
    }
    ensureLinkingSubscription();
    return new Promise<WalletConnectResult>((resolve, reject) => {
      pending = { resolve, reject, challenge };
      const url =
        `${PING_CONNECT_URL}?` +
        new URLSearchParams({
          app: 'iogrid',
          redirect: REDIRECT_LINK,
          challenge: challenge.message,
        }).toString();
      Linking.openURL(url).catch((err: unknown) => {
        pending = null;
        reject(err instanceof Error ? err : new Error(String(err)));
      });
    });
  }
}

export const pingWallet = new PingWallet();
