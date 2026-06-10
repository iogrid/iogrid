// Tests for the Ping PAYMENT (SPL-Approve) surface — Refs #629.
//
// Covers the canonical-contract realignment away from the self-invented
// `ping://topup?…` custom scheme to the Universal-Link
// `https://ping.cash/approve?…` shape:
//   - atomic-amount conversion (9 decimals — canonical per whitepaper)
//   - memo schema `iogrid.v1:vpn:<region>:<days>`
//   - return_url = iogrid://vpn/activated
//   - return-bounce parser (ok=1 success, ok=0&reason=cancel soft cancel,
//     ok=0&reason=<other> hard reject)

import {
  BUY_VPN_LINK_PREFIX,
  GRID_DECIMALS,
  PING_APPROVE_URL,
  VPN_ACTIVATED_RETURN,
  buildBuildApproveUrl,
  buildBuildMemo,
  buildVpnApproveUrl,
  buildVpnConfirmedReturn,
  buildVpnMemo,
  gridToAtomic,
  onVpnApproveReturn,
  parseBuyVpnRequest,
  parseVpnReturn,
} from '../ping-pay';

const VAULT = 'VauLt1111111111111111111111111111111111111';

// -----------------------------------------------------------------------
// atomic conversion
// -----------------------------------------------------------------------

describe('gridToAtomic — 9-decimal $GRID conversion', () => {
  it('multiplies whole $GRID by 10^9 (canonical: whitepaper + billing-svc)', () => {
    expect(GRID_DECIMALS).toBe(9);
    expect(gridToAtomic(250)).toBe('250000000000');
    expect(gridToAtomic(1)).toBe('1000000000');
    expect(gridToAtomic(0)).toBe('0');
  });

  it('keeps precision on large amounts (BigInt, not float)', () => {
    expect(gridToAtomic(10000)).toBe('10000000000000');
    expect(gridToAtomic(2_000_000)).toBe('2000000000000000');
  });

  it('rejects fractional / negative / non-finite amounts', () => {
    expect(() => gridToAtomic(1.5)).toThrow(/invalid \$GRID amount/);
    expect(() => gridToAtomic(-5)).toThrow(/invalid \$GRID amount/);
    expect(() => gridToAtomic(NaN)).toThrow(/invalid \$GRID amount/);
    expect(() => gridToAtomic(Infinity)).toThrow(/invalid \$GRID amount/);
  });
});

// -----------------------------------------------------------------------
// memo schema
// -----------------------------------------------------------------------

describe('buildVpnMemo — iogrid.v1:vpn:<region>:<days>', () => {
  it('builds the versioned colon-delimited memo', () => {
    expect(buildVpnMemo('us-east', 30)).toBe('iogrid.v1:vpn:us-east:30');
    expect(buildVpnMemo('global', 7)).toBe('iogrid.v1:vpn:global:7');
  });

  it('rejects a region containing a colon or whitespace (schema corruption)', () => {
    expect(() => buildVpnMemo('us:east', 30)).toThrow(/invalid region/);
    expect(() => buildVpnMemo('us east', 30)).toThrow(/invalid region/);
    expect(() => buildVpnMemo('', 30)).toThrow(/invalid region/);
  });

  it('rejects non-positive / fractional days', () => {
    expect(() => buildVpnMemo('us-east', 0)).toThrow(/invalid days/);
    expect(() => buildVpnMemo('us-east', -1)).toThrow(/invalid days/);
    expect(() => buildVpnMemo('us-east', 1.5)).toThrow(/invalid days/);
  });
});

// -----------------------------------------------------------------------
// approve URL builder
// -----------------------------------------------------------------------

describe('buildVpnApproveUrl — Universal-Link SPL-Approve shape', () => {
  it('builds the canonical https://ping.cash/approve link with all fields', () => {
    const url = buildVpnApproveUrl({
      grid: 250,
      region: 'us-east',
      days: 30,
      delegate: VAULT,
    });
    expect(url.startsWith(`${PING_APPROVE_URL}?`)).toBe(true);
    const params = new URLSearchParams(url.slice(`${PING_APPROVE_URL}?`.length));
    expect(params.get('token')).toBe('GRID');
    expect(params.get('delegate')).toBe(VAULT);
    expect(params.get('amount')).toBe('250000000000'); // 250 GRID @ 9 decimals
    expect(params.get('memo')).toBe('iogrid.v1:vpn:us-east:30');
    expect(params.get('return_url')).toBe(VPN_ACTIVATED_RETURN);
  });

  it('is NOT a custom scheme (no ping:// — Universal Link only)', () => {
    const url = buildVpnApproveUrl({ grid: 1, region: 'global', days: 1, delegate: VAULT });
    expect(url.startsWith('https://')).toBe(true);
    expect(url.includes('ping://')).toBe(false);
  });

  it('falls back to EXPO_PUBLIC_IOGRID_VPN_VAULT env when no delegate passed', () => {
    const prev = process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT;
    process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT = VAULT;
    try {
      const url = buildVpnApproveUrl({ grid: 100, region: 'eu', days: 30 });
      expect(new URLSearchParams(url.split('?')[1]).get('delegate')).toBe(VAULT);
    } finally {
      if (prev === undefined) delete process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT;
      else process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT = prev;
    }
  });

  it('throws when the delegate vault is unset (CI / pre-vault guard)', () => {
    const prev = process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT;
    delete process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT;
    try {
      expect(() => buildVpnApproveUrl({ grid: 100, region: 'eu', days: 30 })).toThrow(
        /vault delegate is unset/,
      );
    } finally {
      if (prev !== undefined) process.env.EXPO_PUBLIC_IOGRID_VPN_VAULT = prev;
    }
  });
});

// -----------------------------------------------------------------------
// return-bounce parser
// -----------------------------------------------------------------------

describe('parseVpnReturn — return-bounce parsing', () => {
  it('parses a success bounce with signature', () => {
    const r = parseVpnReturn(`${VPN_ACTIVATED_RETURN}?ok=1&signature=abc123`);
    expect(r).toEqual({ ok: true, signature: 'abc123' });
  });

  it('parses a success bounce with no signature (null)', () => {
    const r = parseVpnReturn(`${VPN_ACTIVATED_RETURN}?ok=1`);
    expect(r).toEqual({ ok: true, signature: null });
  });

  it('parses a soft cancel (ok=0&reason=cancel) as re-promptable', () => {
    const r = parseVpnReturn(`${VPN_ACTIVATED_RETURN}?ok=0&reason=cancel`);
    expect(r).toEqual({ ok: false, reason: 'cancel', cancelled: true });
  });

  it('parses a hard reject (ok=0&reason=<other>) as non-cancel failure', () => {
    const r = parseVpnReturn(`${VPN_ACTIVATED_RETURN}?ok=0&reason=insufficient_funds`);
    expect(r).toEqual({ ok: false, reason: 'insufficient_funds', cancelled: false });
  });

  it('defaults missing reason to "unknown" on failure', () => {
    const r = parseVpnReturn(`${VPN_ACTIVATED_RETURN}?ok=0`);
    expect(r).toEqual({ ok: false, reason: 'unknown', cancelled: false });
  });

  it('returns null for foreign deeplinks (so a shared listener can ignore them)', () => {
    expect(parseVpnReturn('iogrid://wallet-callback?source=ping')).toBeNull();
    expect(parseVpnReturn('someotherapp://vpn/activated?ok=1')).toBeNull();
  });
});

// -----------------------------------------------------------------------
// onVpnApproveReturn — listener wiring (via expo-linking mock)
// -----------------------------------------------------------------------

describe('onVpnApproveReturn — deeplink listener', () => {
  it('fires the listener on a matching return bounce and ignores foreign ones', () => {
    jest.isolateModules(() => {
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const mock = require('../../mocks/expo-linking') as typeof import('../../mocks/expo-linking');
      // eslint-disable-next-line @typescript-eslint/no-require-imports
      const mod = require('../ping-pay') as typeof import('../ping-pay');
      mock.__reset();

      const seen: unknown[] = [];
      const unsub = mod.onVpnApproveReturn((r) => seen.push(r));

      // Foreign deeplink → ignored.
      mock.__fireUrl('iogrid://wallet-callback?source=ping');
      expect(seen).toHaveLength(0);

      // Matching success bounce → delivered.
      mock.__fireUrl(`${mod.VPN_ACTIVATED_RETURN}?ok=1&signature=sig`);
      expect(seen).toEqual([{ ok: true, signature: 'sig' }]);

      unsub();
      // After unsubscribe no more events.
      mock.__fireUrl(`${mod.VPN_ACTIVATED_RETURN}?ok=0&reason=cancel`);
      expect(seen).toHaveLength(1);
    });
  });

  describe('Direction B — parseBuyVpnRequest (iogrid://buy-vpn, C-6)', () => {
    const RU = 'https://ping.cash/vpn-confirmed';

    it('parses a valid inbound buy-vpn request', () => {
      const req = parseBuyVpnRequest(
        `${BUY_VPN_LINK_PREFIX}?amount=250000000000&memo=iogrid.v1:vpn:us-east:30&return_url=${encodeURIComponent(RU)}`,
      );
      expect(req).toEqual({
        amountAtomic: '250000000000',
        memo: 'iogrid.v1:vpn:us-east:30',
        returnUrl: RU,
      });
    });

    it('returns null for non buy-vpn deeplinks (shared listener ignores foreign)', () => {
      expect(parseBuyVpnRequest('iogrid://vpn/activated?ok=1')).toBeNull();
      expect(parseBuyVpnRequest('https://ping.cash/approve?x=1')).toBeNull();
    });

    it('rejects a non-integer amount', () => {
      expect(
        parseBuyVpnRequest(`${BUY_VPN_LINK_PREFIX}?amount=1.5&return_url=${encodeURIComponent(RU)}`),
      ).toBeNull();
      expect(
        parseBuyVpnRequest(`${BUY_VPN_LINK_PREFIX}?amount=abc&return_url=${encodeURIComponent(RU)}`),
      ).toBeNull();
    });

    it('SECURITY: rejects a return_url that is not https on an allowed host (open-redirect guard)', () => {
      // wrong host
      expect(
        parseBuyVpnRequest(
          `${BUY_VPN_LINK_PREFIX}?amount=1&return_url=${encodeURIComponent('https://evil.example/steal')}`,
        ),
      ).toBeNull();
      // non-https scheme
      expect(
        parseBuyVpnRequest(
          `${BUY_VPN_LINK_PREFIX}?amount=1&return_url=${encodeURIComponent('http://ping.cash/x')}`,
        ),
      ).toBeNull();
      // missing return_url
      expect(parseBuyVpnRequest(`${BUY_VPN_LINK_PREFIX}?amount=1`)).toBeNull();
    });
  });

  describe('buildVpnConfirmedReturn — Direction-B bounce-back', () => {
    const RU = 'https://ping.cash/vpn-confirmed';
    it('builds ok=1 on success', () => {
      expect(buildVpnConfirmedReturn(RU, true)).toBe(`${RU}?ok=1`);
    });
    it('builds ok=0&reason on failure', () => {
      expect(buildVpnConfirmedReturn(RU, false, { reason: 'cancel' })).toBe(`${RU}?ok=0&reason=cancel`);
    });
    it('appends with & when return_url already has a query', () => {
      expect(buildVpnConfirmedReturn(`${RU}?t=1`, true)).toBe(`${RU}?t=1&ok=1`);
    });
  });
});

// -----------------------------------------------------------------------
// iOS-build pull (G3 Phase D — provider earnings via Ping)
// -----------------------------------------------------------------------

describe('buildBuildMemo — iogrid.v1:build:ios:<spec>', () => {
  it('builds the canonical build memo', () => {
    expect(buildBuildMemo('iogrid@main')).toBe('iogrid.v1:build:ios:iogrid@main');
  });
  it('rejects a spec smuggling a colon or whitespace', () => {
    expect(() => buildBuildMemo('a:b')).toThrow(/invalid build spec/);
    expect(() => buildBuildMemo('a b')).toThrow(/invalid build spec/);
    expect(() => buildBuildMemo('')).toThrow(/invalid build spec/);
  });
});

describe('buildBuildApproveUrl — Ping SPL-Approve for an iOS build', () => {
  it('builds the approve URL with token=GRID, atomic amount, build memo', () => {
    const url = buildBuildApproveUrl({ grid: 5, spec: 'iogrid@main', delegate: VAULT });
    expect(url.startsWith(`${PING_APPROVE_URL}?`)).toBe(true);
    const q = new URLSearchParams(url.split('?')[1]);
    expect(q.get('token')).toBe('GRID');
    expect(q.get('delegate')).toBe(VAULT);
    expect(q.get('amount')).toBe(gridToAtomic(5));
    expect(q.get('memo')).toBe('iogrid.v1:build:ios:iogrid@main');
    expect(q.get('return_url')).toBe('iogrid://build/paid');
  });
  it('throws when the build vault delegate is unset (CI guard)', () => {
    const prev = process.env.EXPO_PUBLIC_IOGRID_BUILD_VAULT;
    delete process.env.EXPO_PUBLIC_IOGRID_BUILD_VAULT;
    expect(() => buildBuildApproveUrl({ grid: 5, spec: 'iogrid@main' })).toThrow(
      /build vault delegate is unset/,
    );
    if (prev !== undefined) process.env.EXPO_PUBLIC_IOGRID_BUILD_VAULT = prev;
  });
});
