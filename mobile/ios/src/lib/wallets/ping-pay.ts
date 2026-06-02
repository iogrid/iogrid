// Ping PAYMENT (approve) surface — Refs #629.
//
// This is the PAYMENT surface, distinct from the wallet-BIND surface in
// ping.ts (PingWallet.connectAndSign, which performs ed25519 identity
// binding). Do not conflate the two: bind proves wallet ownership; this
// module launches an SPL "Approve (delegate)" payment so iogrid can pull
// $GRID for a VPN top-up / service activation.
//
// ── Canonical contract (Ping repo: docs/coordination/iogrid-ping-integration.md)
//
// Transport = Apple **Universal Links** (https://ping.cash/approve?…), NOT
// a custom scheme. Ping deliberately chose Universal Links so iOS routes
// straight into the Ping app WITHOUT the "Open in Ping?" interstitial that
// a `ping://` custom scheme would trigger. (Our prior `ping://topup?…`
// shape was self-invented and is being retired here.)
//
// Primitive = SPL Approve (delegate). Launch shape:
//
//   https://ping.cash/approve
//     ?token=GRID
//     &delegate=<vault pubkey>
//     &amount=<atomic>            // $GRID has 6 decimals → N * 10^6
//     &memo=iogrid.v1:vpn:<region>:<days>
//     &return_url=iogrid://vpn/activated
//
// Return bounce (Ping → iogrid, via our registered `iogrid` scheme):
//   success: iogrid://vpn/activated?ok=1&signature=<sig>
//   cancel:  iogrid://vpn/activated?ok=0&reason=cancel
//
// ── Still blocked on Ping (do NOT build past these):
//   * C-8 signature verification (RPC poll vs webhook) is UNDECIDED on
//     Ping's side. verifyApprovalBestEffort() below is a provisional
//     RPC getTransaction poll, clearly marked, pending their ruling.
//   * The delegate vault pubkey is env-indirected (EXPO_PUBLIC_IOGRID_VPN_VAULT)
//     and may be empty in CI / until the real vault address lands.

import * as Linking from 'expo-linking';

/** $GRID SPL decimals per Ping's contract. 250 $GRID → 250_000_000. */
export const GRID_DECIMALS = 6;

/** Universal-Link base for the Ping SPL-Approve surface. */
export const PING_APPROVE_URL = 'https://ping.cash/approve';

/** The deep-link our app is bounced back to on completion/cancel. */
export const VPN_ACTIVATED_RETURN = 'iogrid://vpn/activated';

/**
 * Convert a whole-token $GRID amount to atomic units (6 decimals).
 *
 * Uses integer/BigInt math — never float multiplication — so we don't
 * lose precision on large amounts. Accepts integer token amounts only
 * (the top-up UI only offers integer $GRID); rejects negative / NaN /
 * non-finite / fractional inputs so a malformed amount can never produce
 * a silently-wrong on-chain delegation.
 */
export function gridToAtomic(grid: number): string {
  if (!Number.isFinite(grid) || grid < 0 || !Number.isInteger(grid)) {
    throw new Error(`ping-pay: invalid $GRID amount: ${grid}`);
  }
  return (BigInt(grid) * 10n ** BigInt(GRID_DECIMALS)).toString();
}

/**
 * Build the Ping memo: `iogrid.v1:vpn:<region>:<days>`. The schema is a
 * versioned, colon-delimited tag the iogrid backend parses to learn what
 * the approved pull is FOR. region/days are validated to avoid smuggling
 * a `:` that would corrupt the schema.
 */
export function buildVpnMemo(region: string, days: number): string {
  if (!region || /[:\s]/.test(region)) {
    throw new Error(`ping-pay: invalid region for memo: "${region}"`);
  }
  if (!Number.isInteger(days) || days <= 0) {
    throw new Error(`ping-pay: invalid days for memo: ${days}`);
  }
  return `iogrid.v1:vpn:${region}:${days}`;
}

/** Resolve the delegate vault pubkey. Empty in CI / until the vault lands. */
export function vpnVault(): string {
  const v = process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT;
  return v && v.length > 0 ? v : '';
}

export interface VpnApproveRequest {
  /** Whole $GRID amount (integer). Converted to atomic internally. */
  grid: number;
  /** VPN region tag, e.g. "us-east". Goes into the memo. */
  region: string;
  /** Subscription length in days. Goes into the memo. */
  days: number;
  /**
   * Delegate vault pubkey. Defaults to {@link vpnVault} (env-indirected).
   * Override only for tests / non-VPN vaults.
   */
  delegate?: string;
}

/**
 * Build the canonical Ping SPL-Approve Universal Link for a VPN top-up.
 *
 * Throws if the delegate vault is unset — callers MUST guard this (the
 * vault is empty in CI / until the real address lands) and surface a
 * "not yet available" message rather than launch a delegate-less approve.
 */
export function buildVpnApproveUrl(req: VpnApproveRequest): string {
  const delegate = req.delegate ?? vpnVault();
  if (!delegate) {
    throw new Error(
      'ping-pay: VPN vault delegate is unset (EXPO_PUBLIC_IOGRID_VPN_VAULT)',
    );
  }
  const params = new URLSearchParams({
    token: 'GRID',
    delegate,
    amount: gridToAtomic(req.grid),
    memo: buildVpnMemo(req.region, req.days),
    return_url: VPN_ACTIVATED_RETURN,
  });
  return `${PING_APPROVE_URL}?${params.toString()}`;
}

// -----------------------------------------------------------------------
// Return-bounce parsing + handler (C-10 cancel UX)
// -----------------------------------------------------------------------

/** Parsed result of the `iogrid://vpn/activated?…` return bounce. */
export type VpnReturnResult =
  | { ok: true; signature: string | null }
  | {
      ok: false;
      /**
       * Distinguishes a soft back-out the user can retry from a hard
       * reject. `cancel` (the documented value) → re-promptable; any
       * other reason → treat as a hard failure surfaced verbatim.
       */
      reason: string;
      cancelled: boolean;
    };

/**
 * Parse a `iogrid://vpn/activated` return-bounce URL. Returns null for any
 * URL that is NOT the VPN-activated return (so a shared `iogrid://`
 * listener can ignore foreign deeplinks — same discriminator pattern as
 * ping.ts's `source=ping` guard).
 */
export function parseVpnReturn(url: string): VpnReturnResult | null {
  if (!url.startsWith(VPN_ACTIVATED_RETURN)) return null;
  const parsed = Linking.parse(url);
  const params = (parsed.queryParams ?? {}) as Record<string, string>;
  if (params.ok === '1') {
    return { ok: true, signature: params.signature ?? null };
  }
  const reason = params.reason ?? 'unknown';
  return { ok: false, reason, cancelled: reason === 'cancel' };
}

type ReturnListener = (r: VpnReturnResult) => void;

let returnSub: { remove(): void } | null = null;

/**
 * Subscribe to VPN-approve return bounces. Mirrors the
 * ensureLinkingSubscription pattern in ping.ts: idempotent, and the
 * handler ignores non-matching deeplinks via parseVpnReturn() → null.
 *
 * Returns an unsubscribe fn so a screen can register on mount + clean up
 * on unmount.
 */
export function onVpnApproveReturn(listener: ReturnListener): () => void {
  const sub = Linking.addEventListener('url', (event: { url: string }) => {
    const result = parseVpnReturn(event.url);
    if (result) listener(result);
  });
  // Keep a module-level handle too so we never leak duplicate subs if a
  // caller forgets to unsubscribe (matches ping.ts singleton style).
  returnSub = sub;
  return () => {
    sub.remove();
    if (returnSub === sub) returnSub = null;
  };
}

// -----------------------------------------------------------------------
// Signature verification — PROVISIONAL (Ping C-8 undecided)
// -----------------------------------------------------------------------

/**
 * BEST-EFFORT on-chain confirmation of an approve signature.
 *
 * ⚠️ PROVISIONAL — pending Ping's C-8 decision (RPC poll vs webhook).
 * This is a stub that polls Solana `getTransaction` for the signature
 * returned in the success bounce. It is intentionally minimal: it only
 * confirms the tx landed on-chain (err === null). It does NOT yet verify
 * the delegate/amount/memo match our request, because Ping has not
 * finalised whether iogrid verifies client-side (this path) or whether
 * Ping posts a server-side webhook to the coordinator. Do NOT build the
 * full verification here until C-8 lands — over-building risks throwing
 * away work if Ping picks the webhook path.
 *
 * Returns:
 *   'confirmed'   — tx found, no error
 *   'failed'      — tx found but errored on-chain
 *   'pending'     — not yet visible after the poll budget (caller may retry)
 *   'unsupported' — no signature to check (e.g. cancel bounce)
 */
export type ApprovalStatus = 'confirmed' | 'failed' | 'pending' | 'unsupported';

function rpcURL(): string {
  return process.env.EXPO_PUBLIC_SOLANA_RPC_URL ?? 'https://api.devnet.solana.com';
}

export async function verifyApprovalBestEffort(
  signature: string | null,
  opts: { attempts?: number; intervalMs?: number } = {},
): Promise<ApprovalStatus> {
  if (!signature) return 'unsupported';
  const attempts = opts.attempts ?? 5;
  const intervalMs = opts.intervalMs ?? 2000;

  for (let i = 0; i < attempts; i++) {
    try {
      const res = await fetch(rpcURL(), {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
          jsonrpc: '2.0',
          id: 1,
          method: 'getTransaction',
          params: [
            signature,
            { commitment: 'confirmed', maxSupportedTransactionVersion: 0 },
          ],
        }),
      });
      const json = (await res.json()) as {
        result?: { meta?: { err: unknown } } | null;
      };
      const result = json.result;
      if (result) {
        // TODO(#629 C-8): once Ping decides RPC-poll vs webhook, assert
        // delegate/amount/memo here (or delete this path for webhook).
        return result.meta?.err == null ? 'confirmed' : 'failed';
      }
    } catch {
      // Network blip — fall through to the next poll attempt.
    }
    if (i < attempts - 1) {
      await new Promise((r) => setTimeout(r, intervalMs));
    }
  }
  return 'pending';
}
