// Tests for $GRID balance fetcher (Refs #585 — Track 5).
//
// Covered edge cases (the ones that WILL hit production once the
// $GRID mint deploys + real wallets hit the wallet card):
//
//   4. Empty wallet — wallet exists but has no $GRID token account
//      → returns { amountAtoms: 0n, uiAmount: 0, decimals: 9, ... }.
//      Critically NOT null/undefined/throw — the wallet card needs a
//      first-class zero, not a "balance unknown" stub.
//   5. RPC 429 rate-limit — fetch resolves with res.ok=false; the
//      function MUST throw a structured error so React Query's
//      retry-with-backoff path engages. The error message must
//      include the HTTP status code for observability.
//   6. Mint-not-configured short-circuit — when EXPO_PUBLIC_GRID_TOKEN_MINT
//      is empty (i.e. Track 5 hasn't shipped yet), returns null without
//      issuing any RPC call. This is the v1-staging path; the wallet
//      card renders "balance unavailable" instead of zero.
//
// We mock `fetch` globally; the function under test uses `await fetch()`
// directly without any wrapper layer.

import { fetchGridBalance, formatGridBalance } from '../grid_balance';

// Save the env so test-cases that toggle EXPO_PUBLIC_GRID_TOKEN_MINT
// can restore it on teardown.
const ORIGINAL_ENV = { ...process.env };

const TEST_MINT = 'GRiDmint1111111111111111111111111111111111';
const TEST_WALLET = 'FwxQ87aB6h7iJfXz4Yj2KwGhKpRtVUaB3CdNoSqUVbCp';

type FetchSpy = jest.Mock<Promise<Response>, [input: any, init?: any]>;

function installFetchMock(): FetchSpy {
  const spy = jest.fn() as unknown as FetchSpy;
  // Cast through unknown so TS doesn't fight the global type.
  (globalThis as unknown as { fetch: FetchSpy }).fetch = spy;
  return spy;
}

beforeEach(() => {
  process.env.EXPO_PUBLIC_GRID_TOKEN_MINT = TEST_MINT;
  process.env.EXPO_PUBLIC_SOLANA_RPC_URL = 'https://test.rpc/';
});

afterEach(() => {
  process.env = { ...ORIGINAL_ENV };
});

// -----------------------------------------------------------------------
// 4. Empty wallet — zero $GRID
// -----------------------------------------------------------------------

describe('fetchGridBalance — empty wallet path', () => {
  it('returns a 0n balance (not null/undefined/throw) when wallet has no token account', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue({
      ok: true,
      status: 200,
      // value=[] is the canonical Solana RPC shape for "no token
      // accounts match this owner × mint pair".
      json: async () => ({ jsonrpc: '2.0', id: 1, result: { value: [] } }),
    } as unknown as Response);

    const balance = await fetchGridBalance(TEST_WALLET);
    expect(balance).not.toBeNull();
    expect(balance).not.toBeUndefined();
    expect(balance!.amountAtoms).toBe(0n);
    expect(balance!.uiAmount).toBe(0);
    expect(balance!.decimals).toBe(9);
    expect(typeof balance!.fetchedAt).toBe('number');
  });

  it('sums across multiple token accounts (defensive against duplicate ATAs)', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        jsonrpc: '2.0',
        id: 1,
        result: {
          value: [
            {
              account: {
                data: {
                  parsed: {
                    info: {
                      tokenAmount: {
                        amount: '100000000000',
                        decimals: 9,
                        uiAmount: 100,
                      },
                    },
                  },
                },
              },
            },
            {
              account: {
                data: {
                  parsed: {
                    info: {
                      tokenAmount: {
                        amount: '50000000000',
                        decimals: 9,
                        uiAmount: 50,
                      },
                    },
                  },
                },
              },
            },
          ],
        },
      }),
    } as unknown as Response);

    const balance = await fetchGridBalance(TEST_WALLET);
    expect(balance!.amountAtoms).toBe(150000000000n);
    expect(balance!.uiAmount).toBe(150);
    expect(balance!.decimals).toBe(9);
  });

  it('issues the documented getTokenAccountsByOwner RPC payload', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ jsonrpc: '2.0', id: 1, result: { value: [] } }),
    } as unknown as Response);

    await fetchGridBalance(TEST_WALLET);
    expect(spy).toHaveBeenCalledTimes(1);
    const [url, init] = spy.mock.calls[0];
    expect(url).toBe('https://test.rpc/');
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/json');
    const body = JSON.parse(init.body as string);
    expect(body.method).toBe('getTokenAccountsByOwner');
    expect(body.params[0]).toBe(TEST_WALLET);
    expect(body.params[1]).toEqual({ mint: TEST_MINT });
  });
});

// -----------------------------------------------------------------------
// 5. RPC 429 rate-limit + structured-error propagation
// -----------------------------------------------------------------------

describe('fetchGridBalance — RPC error propagation', () => {
  it('throws a structured "HTTP 429" error so React Query can retry-with-backoff', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue({
      ok: false,
      status: 429,
      json: async () => ({ error: 'rate-limit' }),
    } as unknown as Response);

    await expect(fetchGridBalance(TEST_WALLET)).rejects.toThrow(/HTTP 429/);
    // The function must NOT swallow the failure into a null return —
    // null is reserved for the "mint not yet configured" short-circuit
    // path (test below), and would mask transient rate-limit failures
    // as a stable empty balance.
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('throws on HTTP 500 (generic upstream Solana RPC failure)', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => ({}),
    } as unknown as Response);

    await expect(fetchGridBalance(TEST_WALLET)).rejects.toThrow(/HTTP 500/);
  });

  it('throws when the RPC response carries a structured `error` field', async () => {
    const spy = installFetchMock();
    spy.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        jsonrpc: '2.0',
        id: 1,
        error: { code: -32602, message: 'Invalid param: Invalid public key' },
      }),
    } as unknown as Response);

    await expect(fetchGridBalance(TEST_WALLET)).rejects.toThrow(
      /solana rpc: Invalid param: Invalid public key/,
    );
  });

  it('propagates fetch() network errors (DNS / TCP reset) to the caller', async () => {
    const spy = installFetchMock();
    spy.mockRejectedValue(new Error('network: ENETUNREACH'));

    await expect(fetchGridBalance(TEST_WALLET)).rejects.toThrow(
      /ENETUNREACH/,
    );
  });
});

// -----------------------------------------------------------------------
// 6. Mint-not-configured short-circuit (no RPC call)
// -----------------------------------------------------------------------

describe('fetchGridBalance — mint not configured', () => {
  it('returns null when EXPO_PUBLIC_GRID_TOKEN_MINT is empty without issuing a fetch', async () => {
    process.env.EXPO_PUBLIC_GRID_TOKEN_MINT = '';
    const spy = installFetchMock();

    const result = await fetchGridBalance(TEST_WALLET);
    expect(result).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });

  it('returns null when EXPO_PUBLIC_GRID_TOKEN_MINT is undefined', async () => {
    delete process.env.EXPO_PUBLIC_GRID_TOKEN_MINT;
    const spy = installFetchMock();

    const result = await fetchGridBalance(TEST_WALLET);
    expect(result).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });
});

// -----------------------------------------------------------------------
// formatGridBalance — UI helper smoke
// -----------------------------------------------------------------------

describe('formatGridBalance', () => {
  it('renders null/undefined as "— $GRID"', () => {
    expect(formatGridBalance(null)).toBe('— $GRID');
    expect(formatGridBalance(undefined)).toBe('— $GRID');
  });

  it('trims trailing zeros for whole-token balances', () => {
    expect(
      formatGridBalance({
        amountAtoms: 100000000000n,
        uiAmount: 100,
        decimals: 9,
        fetchedAt: 0,
      }),
    ).toBe('100 $GRID');
  });

  it('keeps fractional digits up to 4 places for sub-unit balances', () => {
    expect(
      formatGridBalance({
        amountAtoms: 432500000000n,
        uiAmount: 432.5,
        decimals: 9,
        fetchedAt: 0,
      }),
    ).toBe('432.5 $GRID');
  });

  it('formats zero balance cleanly (the empty-wallet UI surface)', () => {
    expect(
      formatGridBalance({
        amountAtoms: 0n,
        uiAmount: 0,
        decimals: 9,
        fetchedAt: 0,
      }),
    ).toBe('0 $GRID');
  });
});
