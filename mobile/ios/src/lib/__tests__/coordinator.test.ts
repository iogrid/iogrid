// Tests for the coordinator HTTP client (Refs #573, #588, #605, #690).
//
// This is the mobile app's entire RPC surface to vpn-svc, and it sits
// squarely in the recurring web↔coordinator serialization bug class
// (#630/#675/#685/#686): the server speaks snake_case proto3-JSON, the
// app wants camelCase, and EVERY field is hand-mapped. A dropped or
// mis-spelled key here doesn't throw — it silently yields `undefined`,
// which is exactly the failure-masking shape that shipped "$0.00",
// "No identifiers", and a "/vpn/undefined" 404 to production. These
// tests pin the mapping so a rename on either side fails loudly in CI.
//
// The two highest-stakes behaviours covered:
//   - requestMobileSession's 503 path (the #690 fresh-install connect
//     story): Retry-After HEADER must win over the body field (RFC 7231
//     §7.1.3), and an empty/non-JSON 503 body must NOT throw.
//   - getSession sends the account number via the X-API-Key HEADER, never
//     in the URL (EPIC #566 reviewer MAJOR-3: the account number IS the
//     credential under the Mullvad model; a URL param leaks it to Traefik
//     logs / URL caches / OS Console captures).
//
// `fetch` is mocked globally (same approach as grid_balance.test).
// `expo-constants` is mocked locally with a real coordinatorURL so the
// base URL is deterministic, rather than leaning on the empty-module
// proxy whose stringification is undefined.

jest.mock('expo-constants', () => ({
  __esModule: true,
  default: { expoConfig: { extra: { coordinatorURL: 'https://test.coordinator' } } },
}));

import {
  CoordinatorError,
  getSession,
  listRegions,
  requestMobileSession,
  requestSession,
  topProvidersInRegion,
} from '../coordinator';

type FetchSpy = jest.Mock<Promise<Response>, [input: any, init?: any]>;

function installFetchMock(): FetchSpy {
  const spy = jest.fn() as unknown as FetchSpy;
  (globalThis as unknown as { fetch: FetchSpy }).fetch = spy;
  return spy;
}

/**
 * Build a minimal Response-like object. `headers` is an optional map of
 * header name → value; `.get()` returns null for anything absent (the
 * real Headers contract), which is what the 503-fallback path relies on.
 */
function fakeResponse(opts: {
  ok?: boolean;
  status: number;
  json?: unknown;
  jsonThrows?: boolean;
  headers?: Record<string, string>;
}): Response {
  const headers = opts.headers ?? {};
  return {
    ok: opts.ok ?? (opts.status >= 200 && opts.status < 300),
    status: opts.status,
    headers: {
      get: (name: string) => headers[name] ?? null,
    },
    json: async () => {
      if (opts.jsonThrows) throw new SyntaxError('Unexpected end of JSON input');
      return opts.json;
    },
  } as unknown as Response;
}

const lastCall = (spy: FetchSpy) => spy.mock.calls[spy.mock.calls.length - 1];
const lastUrl = (spy: FetchSpy): string => String(lastCall(spy)[0]);
const lastInit = (spy: FetchSpy): RequestInit => lastCall(spy)[1] ?? {};
const lastBody = (spy: FetchSpy): any => JSON.parse(String(lastInit(spy).body));

// ── listRegions ──────────────────────────────────────────────────

describe('listRegions', () => {
  it('maps snake_case wire fields → camelCase (the #630 bug class)', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 200,
        json: {
          regions: [
            { region: 'us-east', healthy_providers: 7, total_providers: 9 },
            { region: 'eu-west', healthy_providers: 0, total_providers: 4 },
          ],
        },
      }),
    );
    const out = await listRegions();
    expect(out).toEqual([
      { region: 'us-east', healthyProviders: 7, totalProviders: 9 },
      { region: 'eu-west', healthyProviders: 0, totalProviders: 4 },
    ]);
    expect(lastUrl(spy)).toContain('/v1/vpn/regions');
  });

  it('returns [] (never throws) when the regions field is absent', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 200, json: {} }));
    await expect(listRegions()).resolves.toEqual([]);
  });

  it('throws CoordinatorError with the HTTP status on non-2xx', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 503, ok: false, json: {} }));
    await expect(listRegions()).rejects.toThrow(CoordinatorError);
    await expect(listRegions()).rejects.toThrow('503');
  });
});

// ── topProvidersInRegion ─────────────────────────────────────────

describe('topProvidersInRegion', () => {
  it('maps provider fields + defaults a missing median_rtt_ms to null', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 200,
        json: {
          providers: [
            {
              provider_id: 'prov-1',
              wg_public_key: 'pubkey1',
              candidate_set: [{ type: 'host', address: '1.2.3.4', port: 51820 }],
              median_rtt_ms: 42,
            },
            { provider_id: 'prov-2', wg_public_key: 'pubkey2' }, // no candidates, no rtt
          ],
        },
      }),
    );
    const out = await topProvidersInRegion('us-east', 2);
    expect(out[0]).toEqual({
      providerId: 'prov-1',
      wgPublicKey: 'pubkey1',
      candidateSet: [{ type: 'host', address: '1.2.3.4', port: 51820 }],
      medianRttMs: 42,
    });
    expect(out[1].candidateSet).toEqual([]);
    expect(out[1].medianRttMs).toBeNull();
  });

  it('URL-encodes the region and forwards the limit', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 200, json: { providers: [] } }));
    await topProvidersInRegion('ap south/1', 5);
    const url = lastUrl(spy);
    expect(url).toContain('/v1/vpn/regions/ap%20south%2F1/providers');
    expect(url).toContain('limit=5');
  });
});

// ── requestSession ───────────────────────────────────────────────

describe('requestSession', () => {
  it('maps the session body + defaults quota_state to UNSPECIFIED', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 200,
        json: { session_id: 'sess-1', state: 'ACTIVE', region: 'us-east' },
      }),
    );
    const out = await requestSession('key', 'cust', 'auto');
    expect(out).toEqual({
      sessionId: 'sess-1',
      state: 'ACTIVE',
      region: 'us-east',
      quotaState: 'QUOTA_STATE_UNSPECIFIED',
      bytesIn: 0,
      bytesOut: 0,
    });
    // body carries the snake_case request shape the server expects
    expect(lastBody(spy)).toEqual({ api_key: 'key', customer_id: 'cust', region: 'auto' });
  });

  it('returns a synthetic EXHAUSTED state (does NOT throw) on 429', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({ status: 429, ok: false, json: { quota_state: 'QUOTA_STATE_EXHAUSTED' } }),
    );
    const out = await requestSession('key', 'cust', 'auto');
    expect(out.state).toBe('EXHAUSTED');
    expect(out.quotaState).toBe('QUOTA_STATE_EXHAUSTED');
    expect(out.sessionId).toBe('');
  });

  it('throws CoordinatorError on a non-429 error status', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 500, ok: false, json: {} }));
    await expect(requestSession('k', 'c', 'auto')).rejects.toThrow('500');
  });
});

// ── requestMobileSession — the #690 fresh-install connect path ────

describe('requestMobileSession', () => {
  const args = {
    apiKey: 'key',
    customerId: 'cust',
    region: 'auto',
    clientPublicKey: 'clientpub',
  };

  it('maps the resolved WG peer config and reports status 201 on success', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 201,
        json: {
          session_id: 'sess-9',
          peer_public_key: 'peerpub',
          peer_endpoint: '5.6.7.8:51820',
          inner_ip: '10.64.0.2',
          region: 'us-east',
        },
      }),
    );
    const out = await requestMobileSession(args);
    expect(out).toEqual({
      sessionId: 'sess-9',
      peerPublicKey: 'peerpub',
      peerEndpoint: '5.6.7.8:51820',
      innerIP: '10.64.0.2',
      region: 'us-east',
      status: 201,
    });
  });

  it('503: prefers the Retry-After HEADER over the body field (RFC 7231 §7.1.3)', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 503,
        ok: false,
        headers: { 'Retry-After': '30' },
        json: { retry_after_sec: 15 }, // body disagrees — header must win
      }),
    );
    const out = await requestMobileSession(args);
    expect(out.status).toBe(503);
    expect(out.retryAfterSec).toBe(30);
    expect(out.sessionId).toBe('');
  });

  it('503: falls back to the body retry_after_sec when no header is present', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({ status: 503, ok: false, json: { retry_after_sec: 15 } }),
    );
    const out = await requestMobileSession(args);
    expect(out.retryAfterSec).toBe(15);
  });

  it('503: an empty / non-JSON body does NOT throw — retryAfterSec is undefined', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 503, ok: false, jsonThrows: true }));
    const out = await requestMobileSession(args);
    expect(out.status).toBe(503);
    expect(out.retryAfterSec).toBeUndefined();
  });

  it('returns a synthetic status-429 result (does NOT throw) on quota exceeded', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 429, ok: false, json: {} }));
    const out = await requestMobileSession(args);
    expect(out.status).toBe(429);
    expect(out.peerPublicKey).toBe('');
  });

  it('throws CoordinatorError on an unexpected error status (e.g. 401)', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 401, ok: false, json: {} }));
    await expect(requestMobileSession(args)).rejects.toThrow('401');
  });

  it('omits payment_authorization when undefined, includes it when provided', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 201,
        json: { session_id: 's', peer_public_key: 'p', peer_endpoint: 'e' },
      }),
    );
    await requestMobileSession(args);
    expect('payment_authorization' in lastBody(spy)).toBe(false);

    await requestMobileSession({ ...args, paymentAuthorization: { sig: 'abc' } });
    expect(lastBody(spy).payment_authorization).toEqual({ sig: 'abc' });
  });
});

// ── getSession — credential must travel as a header, never the URL ─

describe('getSession', () => {
  it('sends the api key via X-API-Key header and NOT in the URL (#566 MAJOR-3)', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(
      fakeResponse({
        status: 200,
        json: { session_id: 'sess-1', state: 'ACTIVE', region: 'us-east' },
      }),
    );
    await getSession('sess-1', 'super-secret-account-number');
    const url = lastUrl(spy);
    expect(url).toContain('/v1/vpn/sessions/sess-1');
    // the credential must never leak into the URL (logs / caches / Console)
    expect(url).not.toContain('super-secret-account-number');
    expect((lastInit(spy).headers as Record<string, string>)['X-API-Key']).toBe(
      'super-secret-account-number',
    );
  });

  it('throws CoordinatorError on non-2xx', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue(fakeResponse({ status: 404, ok: false, json: {} }));
    await expect(getSession('x', 'k')).rejects.toThrow('404');
  });
});
