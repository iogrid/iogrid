// Phantom Wallet adapter — iOS deeplink protocol.
//
// Phantom's mobile deeplink protocol (the only public option for non-
// browser dapps as of 2026):
//
//   1. Connect:
//        phantom://v1/connect?
//          dapp_encryption_public_key=<base58 ephemeral x25519 pub>
//          &cluster=mainnet-beta
//          &app_url=https://iogrid.org
//          &redirect_link=iogrid://wallet-callback
//      Phantom returns:
//        iogrid://wallet-callback?
//          phantom_encryption_public_key=<base58>
//          &nonce=<base58 24-byte>
//          &data=<base58 NaCl-box ciphertext of {public_key, session}>
//      Mobile decrypts `data` with shared secret =
//        nacl.box.before(phantom_pub, our_priv) — yielding the wallet
//      pubkey + an opaque session token used for subsequent signMessage.
//
//   2. signMessage:
//        phantom://v1/signMessage?
//          dapp_encryption_public_key=<our base58 pub>
//          &nonce=<fresh base58 24-byte>
//          &redirect_link=iogrid://wallet-callback
//          &payload=<base58 NaCl-box of {session, message}>
//      Returns iogrid://wallet-callback with `data` ciphertext of
//        {signature: <base58 sig>}.
//
// We collapse (1) + (2) into a single connectAndSign() flow: connect
// first to get the session, then immediately request the bind-message
// signature. This keeps the UX one tap per wallet.
//
// Why tweetnacl, not @noble/curves: tweetnacl ships compiled with no
// native deps and works under Hermes (RN's JS engine). @noble has
// better cryptographic provenance but pulls a much larger bundle and
// has had Hermes-compat issues. The crypto primitive here (x25519 +
// NaCl secretbox) is well-trodden territory — both libs are safe.

import * as Linking from 'expo-linking';
import nacl from 'tweetnacl';
import bs58 from 'bs58';

import type {
  BindChallenge,
  Wallet,
  WalletConnectResult,
} from './types';

const PHANTOM_CONNECT_URL = 'phantom://v1/connect';
const PHANTOM_SIGN_URL = 'phantom://v1/signMessage';
// iogrid: scheme is the redirect target. Phantom appends the response
// params as query-string fields.
const REDIRECT_LINK = 'iogrid://wallet-callback';
const APP_URL = 'https://iogrid.org';
// SOLANA_RPC_URL points at devnet for v1 staging; mainnet for prod.
// Cluster sent to Phantom must match — mainnet wallets refuse to sign
// for devnet without confirmation prompts (and vice versa).
const PHANTOM_CLUSTER: 'mainnet-beta' | 'devnet' =
  process.env.EXPO_PUBLIC_SOLANA_NETWORK === 'mainnet'
    ? 'mainnet-beta'
    : 'devnet';

/**
 * One-shot ephemeral keypair for a Phantom connect+sign session. We
 * regenerate per attempt so a leaked redirect URL has no useful
 * after-life. The session itself is short-lived (Phantom invalidates
 * after ~5min of idleness) so persisting across launches buys nothing.
 */
function newEphemeralKeypair() {
  return nacl.box.keyPair();
}

/** Resolver registry — at most one outstanding Phantom flow per app. */
type Pending = {
  resolve: (r: WalletConnectResult) => void;
  reject: (e: Error) => void;
  challenge: BindChallenge;
  dappKeypair: nacl.BoxKeyPair;
  // After connect succeeds we move to step 2 (signMessage) and stash
  // the shared secret + session here so the second callback can decode.
  sharedSecret?: Uint8Array;
  session?: string;
  walletPubkey?: string;
};

let pending: Pending | null = null;
let linkingSub: { remove(): void } | null = null;

/** Install (once per process) the deep-link listener that resolves
 *  Phantom callbacks. Idempotent — safe to call repeatedly.
 */
function ensureLinkingSubscription() {
  if (linkingSub) return;
  linkingSub = Linking.addEventListener('url', (event) => {
    try {
      handlePhantomCallback(event.url);
    } catch (err) {
      // Re-route to pending's reject without crashing the app.
      if (pending) {
        const p = pending;
        pending = null;
        p.reject(err instanceof Error ? err : new Error(String(err)));
      }
    }
  });
}

function handlePhantomCallback(url: string) {
  if (!url.startsWith(REDIRECT_LINK)) return;
  if (!pending) return; // stale callback; ignore
  const parsed = Linking.parse(url);
  const params = (parsed.queryParams ?? {}) as Record<string, string>;

  // Phantom returns ?errorCode=... when the user rejected the prompt
  // or the dapp-encryption pubkey mismatch — surface as a user-facing
  // error to render in the connect-wallet UI.
  if (params.errorCode) {
    const p = pending;
    pending = null;
    p.reject(new Error(`phantom: ${params.errorMessage ?? params.errorCode}`));
    return;
  }

  const isConnect = pending.session === undefined;
  if (isConnect) {
    finalizeConnect(params);
  } else {
    finalizeSign(params);
  }
}

function finalizeConnect(params: Record<string, string>) {
  if (!pending) return;
  const { phantom_encryption_public_key: phantomPubB58, nonce, data } = params;
  if (!phantomPubB58 || !nonce || !data) {
    const p = pending;
    pending = null;
    p.reject(new Error('phantom: connect callback missing fields'));
    return;
  }
  const phantomPub = bs58.decode(phantomPubB58);
  const sharedSecret = nacl.box.before(phantomPub, pending.dappKeypair.secretKey);
  const cipher = bs58.decode(data);
  const nonceBytes = bs58.decode(nonce);
  const plain = nacl.box.open.after(cipher, nonceBytes, sharedSecret);
  if (!plain) {
    const p = pending;
    pending = null;
    p.reject(new Error('phantom: connect decrypt failed'));
    return;
  }
  // plaintext is JSON: {public_key: "<base58>", session: "<opaque>"}
  const decoded = JSON.parse(new TextDecoder().decode(plain)) as {
    public_key: string;
    session: string;
  };
  pending.sharedSecret = sharedSecret;
  pending.session = decoded.session;
  pending.walletPubkey = decoded.public_key;

  // Immediately fire step 2 — signMessage for the iogrid bind challenge.
  void requestSign(pending.challenge);
}

function finalizeSign(params: Record<string, string>) {
  if (!pending) return;
  const { nonce, data } = params;
  if (!nonce || !data || !pending.sharedSecret || !pending.walletPubkey) {
    const p = pending;
    pending = null;
    p.reject(new Error('phantom: sign callback missing fields'));
    return;
  }
  const cipher = bs58.decode(data);
  const nonceBytes = bs58.decode(nonce);
  const plain = nacl.box.open.after(cipher, nonceBytes, pending.sharedSecret);
  if (!plain) {
    const p = pending;
    pending = null;
    p.reject(new Error('phantom: sign decrypt failed'));
    return;
  }
  const decoded = JSON.parse(new TextDecoder().decode(plain)) as {
    signature: string;
  };
  const result: WalletConnectResult = {
    address: pending.walletPubkey,
    provider: 'phantom',
    challenge: pending.challenge,
    signatureBase58: decoded.signature,
  };
  const p = pending;
  pending = null;
  p.resolve(result);
}

/** Step 2 — open phantom://v1/signMessage with the bind challenge. */
async function requestSign(challenge: BindChallenge): Promise<void> {
  if (!pending || !pending.sharedSecret || !pending.session) return;
  const messageBytes = new TextEncoder().encode(challenge.message);
  const payloadObj = {
    session: pending.session,
    message: bs58.encode(messageBytes),
  };
  const payloadBytes = new TextEncoder().encode(JSON.stringify(payloadObj));
  const nonceBytes = nacl.randomBytes(24);
  const ciphertext = nacl.box.after(payloadBytes, nonceBytes, pending.sharedSecret);
  const url =
    `${PHANTOM_SIGN_URL}?` +
    new URLSearchParams({
      dapp_encryption_public_key: bs58.encode(pending.dappKeypair.publicKey),
      nonce: bs58.encode(nonceBytes),
      redirect_link: REDIRECT_LINK,
      payload: bs58.encode(ciphertext),
    }).toString();
  await Linking.openURL(url);
}

class PhantomWallet implements Wallet {
  readonly provider = 'phantom' as const;

  async isInstalled(): Promise<boolean> {
    try {
      return await Linking.canOpenURL('phantom://');
    } catch {
      return false;
    }
  }

  appStoreURL(): string {
    // Phantom on the App Store — universal install link.
    return 'https://apps.apple.com/app/phantom-solana-wallet/id1598432977';
  }

  async connectAndSign(challenge: BindChallenge): Promise<WalletConnectResult> {
    if (pending) {
      throw new Error('phantom: another connect flow is already in progress');
    }
    ensureLinkingSubscription();
    const dappKeypair = newEphemeralKeypair();
    return new Promise<WalletConnectResult>((resolve, reject) => {
      pending = { resolve, reject, challenge, dappKeypair };
      const url =
        `${PHANTOM_CONNECT_URL}?` +
        new URLSearchParams({
          dapp_encryption_public_key: bs58.encode(dappKeypair.publicKey),
          cluster: PHANTOM_CLUSTER,
          app_url: APP_URL,
          redirect_link: REDIRECT_LINK,
        }).toString();
      Linking.openURL(url).catch((err: unknown) => {
        pending = null;
        reject(err instanceof Error ? err : new Error(String(err)));
      });
    });
  }
}

/** Singleton — there's only ever one Phantom flow active per app. */
export const phantomWallet = new PhantomWallet();
