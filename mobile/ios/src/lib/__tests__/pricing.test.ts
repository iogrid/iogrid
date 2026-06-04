// Tests for the $GRID↔USD pricing helpers (Refs #594).
//
// A money conversion that's silently off-by-a-factor, renders "$NaN", or
// shows a negative price is the worst regression a top-up screen can
// ship — the user is about to authorize a real Ping payment against this
// number. These pin the ratio (100 $GRID = $1) and the defensive
// formatting so any drift fails in CI rather than on a customer's card.

import { GRID_PER_USD, formatGridAsUsd, gridToUsd } from '../pricing';

describe('gridToUsd', () => {
  it('uses the canonical 100 $GRID = $1 ratio', () => {
    expect(GRID_PER_USD).toBe(100);
    expect(gridToUsd(100)).toBe(1);
    expect(gridToUsd(2500)).toBe(25);
    expect(gridToUsd(10000)).toBe(100);
  });

  it('handles sub-dollar amounts', () => {
    expect(gridToUsd(50)).toBe(0.5);
    expect(gridToUsd(1)).toBe(0.01);
  });

  it('collapses zero / negative / non-finite to 0 (never $NaN or a negative price)', () => {
    expect(gridToUsd(0)).toBe(0);
    expect(gridToUsd(-500)).toBe(0);
    expect(gridToUsd(NaN)).toBe(0);
    expect(gridToUsd(Infinity)).toBe(0);
  });
});

describe('formatGridAsUsd', () => {
  it('always renders two decimals with a leading $', () => {
    expect(formatGridAsUsd(500)).toBe('$5.00');
    expect(formatGridAsUsd(2500)).toBe('$25.00');
    expect(formatGridAsUsd(10000)).toBe('$100.00');
  });

  it('rounds to cents', () => {
    // 1234 $GRID = $12.34 exactly; 1235 = $12.35
    expect(formatGridAsUsd(1234)).toBe('$12.34');
    expect(formatGridAsUsd(1235)).toBe('$12.35');
  });

  it('renders $0.00 for the empty / invalid states (not $NaN)', () => {
    expect(formatGridAsUsd(0)).toBe('$0.00');
    expect(formatGridAsUsd(NaN)).toBe('$0.00');
    expect(formatGridAsUsd(-1)).toBe('$0.00');
  });
});
