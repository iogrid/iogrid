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
  // #738: the per-session tunnel-inner IP, surfaced by GET /sessions/{id}
  // so the app can recover it post-connect (the create response also
  // carries it as `inner_ip`). Empty string when the server hasn't
  // allocated one (legacy/non-mobile sessions).
  innerIP: string;
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
    inner_ip?: string;
  };
  return {
    sessionId: body.session_id,
    state: body.state,
    region: body.region,
    quotaState: (body.quota_state as QuotaState) ?? 'QUOTA_STATE_UNSPECIFIED',
    bytesIn: body.bytes_in ?? 0,
    bytesOut: body.bytes_out ?? 0,
    // #738: GET /sessions/{id} now surfaces the inner IP (empty for the
    // legacy POST /sessions daemon flow, which doesn't allocate one).
    innerIP: body.inner_ip ?? '',
  };
}

// ── Mobile-session bring-up (#588, #605) ─────────────────────────

/**
 * Response from POST /v1/vpn/sessions/mobile — vpn-svc returns the
 * fully resolved WireGuard peer config in one round-trip. Matches
 * the JSON shape in coordinator/services/vpn-svc/internal/server/
 * handlers.go::RequestMobileSession.Handle (commit b73085d8, #605).
 */
export interface MobileSession {
  sessionId: string;
  peerPublicKey: string;
  peerEndpoint: string;
  innerIP: string;
  region: string;
  // Populated when vpn-svc returns 503 (no peer available yet); the
  // JS layer reads this to schedule a retry per the spec.
  retryAfterSec?: number;
  // HTTP status as observed — 201 on success, 503 on no_peer_available,
  // 429 on quota_exceeded. Surfaced so the caller can branch without
  // re-decoding the response.
  status: number;
}

/**
 * POST /v1/vpn/sessions/mobile — single-shot mobile session bring-up.
 *
 * Per #588 DoD this replaces the legacy two-step (POST /sessions →
 * GET /sessions/{id}) with one request that returns the full WG peer
 * config. The 503 + Retry-After path is the DEGRADED scope tested by
 * Maestro flow 10 when no real provider is in the cluster.
 *
 * Emits a debug log line on entry — Maestro greps the simulator
 * console for this marker to assert the JS layer actually issued
 * the POST (cheaper than wiring a Charles proxy into CI).
 */
export async function requestMobileSession(args: {
  apiKey: string;
  customerId: string;
  region: string;
  clientPublicKey: string;
  paymentAuthorization?: unknown;
}): Promise<MobileSession> {
  // Maestro-matchable marker. Keep the prefix stable; tests grep
  // for the literal string `[iogrid/coordinator] POST /v1/vpn/sessions/mobile`.
  console.log(
    `[iogrid/coordinator] POST /v1/vpn/sessions/mobile region=${args.region} customer=${args.customerId}`,
  );
  const res = await fetch(`${baseURL()}/v1/vpn/sessions/mobile`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({
      api_key: args.apiKey,
      customer_id: args.customerId,
      region: args.region,
      client_public_key: args.clientPublicKey,
      ...(args.paymentAuthorization !== undefined
        ? { payment_authorization: args.paymentAuthorization }
        : {}),
    }),
  });
  if (res.status === 503) {
    // DEGRADED path — no healthy provider in the region yet. The
    // server includes retry_after_sec in the body AND a Retry-After
    // header; prefer the header when present (RFC 7231 §7.1.3).
    const headerRetry = res.headers.get('Retry-After');
    let bodyRetry: number | undefined;
    try {
      const body = (await res.json()) as { retry_after_sec?: number };
      bodyRetry = body.retry_after_sec;
    } catch {
      // body might be empty / non-JSON — fall back to header.
    }
    const retryAfterSec = headerRetry ? parseInt(headerRetry, 10) : bodyRetry;
    console.log(
      `[iogrid/coordinator] sessions/mobile 503 — no peer available, retry_after_sec=${retryAfterSec ?? 'unknown'}`,
    );
    return {
      sessionId: '',
      peerPublicKey: '',
      peerEndpoint: '',
      innerIP: '',
      region: args.region,
      retryAfterSec,
      status: 503,
    };
  }
  if (res.status === 429) {
    return {
      sessionId: '',
      peerPublicKey: '',
      peerEndpoint: '',
      innerIP: '',
      region: args.region,
      status: 429,
    };
  }
  if (!res.ok) {
    throw new CoordinatorError(`requestMobileSession: HTTP ${res.status}`);
  }
  const body = (await res.json()) as {
    session_id: string;
    peer_public_key: string;
    peer_endpoint: string;
    inner_ip?: string;
    region?: string;
  };
  return {
    sessionId: body.session_id,
    peerPublicKey: body.peer_public_key,
    peerEndpoint: body.peer_endpoint,
    innerIP: body.inner_ip ?? '',
    region: body.region ?? args.region,
    status: res.status,
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
    inner_ip?: string;
  };
  return {
    sessionId: body.session_id,
    state: body.state,
    region: body.region,
    quotaState: (body.quota_state as QuotaState) ?? 'QUOTA_STATE_UNSPECIFIED',
    bytesIn: body.bytes_in ?? 0,
    bytesOut: body.bytes_out ?? 0,
    // #738: GET /sessions/{id} now surfaces the inner IP (empty for the
    // legacy POST /sessions daemon flow, which doesn't allocate one).
    innerIP: body.inner_ip ?? '',
  };
}

// ── Errors ───────────────────────────────────────────────────────

export class CoordinatorError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CoordinatorError';
  }
}
