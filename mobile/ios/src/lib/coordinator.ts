// Coordinator HTTP client — the mobile app's thin RPC layer over
// vpn-svc's HTTP surface. Stays JSON over fetch (no Connect-RPC) for
// the lowest possible RN bundle size; the backend serves both Connect
// and plain JSON anyway via the existing handlers.
//
// All endpoints are versioned `/v1/vpn/*`; the base URL comes from
// `expo-constants.expoConfig.extra.coordinatorURL` (set in app.json)
// so QA / staging swaps land via EAS build profiles.

import Constants from 'expo-constants';

const DEFAULT_BASE_URL = 'https://api.iogrid.org';

function baseURL(): string {
  const fromConfig = Constants.expoConfig?.extra?.coordinatorURL as string | undefined;
  return fromConfig ?? DEFAULT_BASE_URL;
}

// ── Region list (#571) ───────────────────────────────────────────

/** One row from GET /v1/vpn/regions — matches vpn-svc's response. */
export interface RegionRow {
  region: string;
  healthyProviders: number;
  totalProviders: number;
}

export async function listRegions(): Promise<RegionRow[]> {
  const res = await fetch(`${baseURL()}/v1/vpn/regions`, {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    throw new CoordinatorError(`listRegions: HTTP ${res.status}`);
  }
  const body = (await res.json()) as {
    regions?: Array<{ region: string; healthy_providers: number; total_providers: number }>;
  };
  return (body.regions ?? []).map((r) => ({
    region: r.region,
    healthyProviders: r.healthy_providers,
    totalProviders: r.total_providers,
  }));
}

// ── Top-N providers for a region (#572 client-probe input) ──────

export interface ProviderRow {
  providerId: string;
  wgPublicKey: string;
  candidateSet: Array<{
    type: 'host' | 'srflx' | 'relay';
    address: string;
    port: number;
  }>;
  medianRttMs: number | null;
}

export async function topProvidersInRegion(
  region: string,
  limit = 3,
): Promise<ProviderRow[]> {
  const url = `${baseURL()}/v1/vpn/regions/${encodeURIComponent(region)}/providers?limit=${limit}`;
  const res = await fetch(url, { headers: { Accept: 'application/json' } });
  if (!res.ok) {
    throw new CoordinatorError(`topProvidersInRegion: HTTP ${res.status}`);
  }
  // Sub-agent's #570 ship returned the mobile-probe shape under `?limit=N`.
  // Field names match `proto/iogrid/vpn/v1/regions.proto`.
  const body = (await res.json()) as {
    providers?: Array<{
      provider_id: string;
      wg_public_key: string;
      candidate_set?: Array<{ type: string; address: string; port: number }>;
      median_rtt_ms?: number;
    }>;
  };
  return (body.providers ?? []).map((p) => ({
    providerId: p.provider_id,
    wgPublicKey: p.wg_public_key,
    candidateSet: (p.candidate_set ?? []).map((c) => ({
      type: c.type as 'host' | 'srflx' | 'relay',
      address: c.address,
      port: c.port,
    })),
    medianRttMs: p.median_rtt_ms ?? null,
  }));
}

// ── Sessions (#573 quota_state) ─────────────────────────────────

export type QuotaState =
  | 'QUOTA_STATE_OK'
  | 'QUOTA_STATE_THROTTLED'
  | 'QUOTA_STATE_EXHAUSTED'
  | 'QUOTA_STATE_UNSPECIFIED';

export interface SessionState {
  sessionId: string;
  state: string;
  region: string;
  quotaState: QuotaState;
  bytesIn: number;
  bytesOut: number;
}

/**
 * POST /v1/vpn/sessions — request a new VPN session. The mobile app
 * uses region="auto" by default; the coordinator picks the best
 * provider across all regions (per #570). API key is the customer's
 * Mullvad-style anon ID derived UUID (per #569) — vpn-svc creates
 * the customer row on first sight with tier=FREE.
 */
export async function requestSession(
  apiKey: string,
  customerId: string,
  region: string,
): Promise<SessionState> {
  const res = await fetch(`${baseURL()}/v1/vpn/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({
      api_key: apiKey,
      customer_id: customerId,
      region,
    }),
  });
  if (res.status === 429) {
    // Quota exhausted — body still has quota_state for the banner to read.
    const body = (await res.json()) as { quota_state?: string };
    return {
      sessionId: '',
      state: 'EXHAUSTED',
      region,
      quotaState: (body.quota_state as QuotaState) ?? 'QUOTA_STATE_EXHAUSTED',
      bytesIn: 0,
      bytesOut: 0,
    };
  }
  if (!res.ok) {
    throw new CoordinatorError(`requestSession: HTTP ${res.status}`);
  }
  const body = (await res.json()) as {
    session_id: string;
    state: string;
    region: string;
    quota_state?: string;
    bytes_in?: number;
    bytes_out?: number;
  };
  return {
    sessionId: body.session_id,
    state: body.state,
    region: body.region,
    quotaState: (body.quota_state as QuotaState) ?? 'QUOTA_STATE_UNSPECIFIED',
    bytesIn: body.bytes_in ?? 0,
    bytesOut: body.bytes_out ?? 0,
  };
}

/** GET /v1/vpn/sessions/{id} — fetch session state for heartbeat / banner refresh.
 *
 * EPIC #566 reviewer MAJOR 3: the account number IS the credential under
 * the Mullvad model (#569). Sending it as a URL query param leaks it to
 * Traefik access logs, on-device URL caches, NSURLSession diagnostics, OS
 * Console under VPN diagnostic captures. Sending via X-API-Key header
 * instead keeps it out of all URL-shaped logs while still letting the
 * server (when it adds validation) read it via standard header parsing.
 */
export async function getSession(sessionId: string, apiKey: string): Promise<SessionState> {
  const res = await fetch(`${baseURL()}/v1/vpn/sessions/${encodeURIComponent(sessionId)}`, {
    headers: { Accept: 'application/json', 'X-API-Key': apiKey },
  });
  if (!res.ok) {
    throw new CoordinatorError(`getSession: HTTP ${res.status}`);
  }
  const body = (await res.json()) as {
    session_id: string;
    state: string;
    region: string;
    quota_state?: string;
    bytes_in?: number;
    bytes_out?: number;
  };
  return {
    sessionId: body.session_id,
    state: body.state,
    region: body.region,
    quotaState: (body.quota_state as QuotaState) ?? 'QUOTA_STATE_UNSPECIFIED',
    bytesIn: body.bytes_in ?? 0,
    bytesOut: body.bytes_out ?? 0,
  };
}

// ── Wallet binding (#583 / #584 — Track 2 of EPIC #581) ─────────

/** Payload accepted by POST /v1/identity/wallet/bind. */
export interface BindWalletReq {
  walletAddress: string;
  walletProvider: 'phantom' | 'ping';
  /** "iogrid:bind:<nonce>:<unix_ts>" — same bytes the wallet signed. */
  challenge: string;
  /** Base58-encoded ed25519 signature of `challenge`. */
  signature: string;
}

/** Resolved binding row as returned by the server. */
export interface BoundWallet {
  userId: string;
  walletAddress: string;
  walletProvider: 'phantom' | 'ping';
  boundAt: string;
}

/**
 * Bind the user's chosen wallet to their iogrid account. The server
 * verifies the signature against the address before persisting.
 *
 * Currently the call goes through the public coordinator gateway
 * without an explicit bearer; once Track 1 (Apple sign-in) lands a
 * `Bearer <access_token>` header should be added here. The endpoint
 * already enforces auth — calling without a bearer surfaces a 401.
 */
export async function bindWalletToCustomer(req: BindWalletReq): Promise<BoundWallet> {
  const res = await fetch(`${baseURL()}/v1/identity/wallet/bind`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({
      wallet_address: req.walletAddress,
      wallet_provider: req.walletProvider,
      challenge: req.challenge,
      signature: req.signature,
    }),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { message?: string };
    throw new CoordinatorError(
      `bindWalletToCustomer: HTTP ${res.status}${body.message ? `: ${body.message}` : ''}`,
    );
  }
  const body = (await res.json()) as {
    binding: {
      user_id: string;
      wallet_address: string;
      wallet_provider: 'phantom' | 'ping';
      bound_at: string;
    };
  };
  return {
    userId: body.binding.user_id,
    walletAddress: body.binding.wallet_address,
    walletProvider: body.binding.wallet_provider,
    boundAt: body.binding.bound_at,
  };
}

/** GET /v1/identity/wallet — returns the user's bound wallet or null. */
export async function getBoundWallet(): Promise<BoundWallet | null> {
  const res = await fetch(`${baseURL()}/v1/identity/wallet/`, {
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    throw new CoordinatorError(`getBoundWallet: HTTP ${res.status}`);
  }
  const body = (await res.json()) as {
    binding: {
      user_id: string;
      wallet_address: string;
      wallet_provider: 'phantom' | 'ping';
      bound_at: string;
    } | null;
  };
  if (!body.binding) return null;
  return {
    userId: body.binding.user_id,
    walletAddress: body.binding.wallet_address,
    walletProvider: body.binding.wallet_provider,
    boundAt: body.binding.bound_at,
  };
}

/** PATCH /v1/identity/wallet/unbind — Settings → Wallet → Switch. */
export async function unbindWallet(): Promise<void> {
  const res = await fetch(`${baseURL()}/v1/identity/wallet/unbind`, {
    method: 'PATCH',
    headers: { Accept: 'application/json' },
  });
  if (!res.ok) {
    throw new CoordinatorError(`unbindWallet: HTTP ${res.status}`);
  }
}

// ── Errors ───────────────────────────────────────────────────────

export class CoordinatorError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CoordinatorError';
  }
}
