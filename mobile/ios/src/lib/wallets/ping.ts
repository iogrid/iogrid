// Ping Wallet (openova-group) adapter.
//
// Ping does not publish an "external wallet connect" protocol yet —
// the ping mobile app uses scheme `cash://` internally for its own
// routes (verified against /home/openova/repos/ping/apps/mobile/app.json).
// Track 2 of EPIC #581 documents the deeplink CONTRACT that the ping
// app must honor for the iogrid bind to work:
//
//   Request:  ping://wallet/connect?
//               app=iogrid
//               &redirect=iogrid://wallet-callback
//               &challenge=<urlencoded "iogrid:bind:<nonce>:<ts>">
//   Response: iogrid://wallet-callback?
//               source=ping
//               &address=<base58 ed25519 pubkey>
//               &signature=<base58 ed25519 signature of the challenge>
//
// Implementing this on the ping side is tracked in a follow-up issue
// against the openova-io/ping repo. Until ping ships the receiver, the
// adapter still launches the deeplink — if ping isn't installed we
// surface "Get Ping" via the App Store; if ping IS installed but does
// not implement the route yet, the user sees a wallet app that does
// nothing useful, which is acceptable v1 because the founder hasn't
// merged ping's outbound side either.
//
// The verification half is identical to Phantom — same SIWS-style
// ed25519 signature over the same bind challenge — so the server side
// (internal/wallet/solana_sig_verify.go) treats the two interchangeably
// and only the `wallet_provider` enum value differs in the bind row.

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
