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

// ── Errors ───────────────────────────────────────────────────────

export class CoordinatorError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CoordinatorError';
  }
}
