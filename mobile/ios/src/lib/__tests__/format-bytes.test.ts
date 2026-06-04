// Tests for the live-session byte formatter (Refs #580 — TC-25).
//
// The ↓/↑ counters on the connected screen are the user's proof the
// tunnel is actually carrying their traffic. An off-by-one on a 1024
// threshold or the wrong decimal precision silently mis-states how much
// data they've pushed — these pin every unit boundary + the defensive
// collapse so a garbage stat frame can't render "NaN B".

import { formatBytes } from '../format-bytes';

describe('formatBytes', () => {
  it('shows whole bytes below 1 KiB', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1023)).toBe('1023 B');
  });

  it('crosses to KB at exactly 1024 (one decimal)', () => {
    expect(formatBytes(1024)).toBe('1.0 KB');
    expect(formatBytes(1536)).toBe('1.5 KB');
  });

  it('crosses to MB at exactly 1024² (one decimal)', () => {
    expect(formatBytes(1024 * 1024)).toBe('1.0 MB');
    expect(formatBytes(1024 * 1024 * 5)).toBe('5.0 MB');
  });

  it('crosses to GB at exactly 1024³ (two decimals)', () => {
    expect(formatBytes(1024 * 1024 * 1024)).toBe('1.00 GB');
    expect(formatBytes(1024 * 1024 * 1024 * 2.5)).toBe('2.50 GB');
  });

  it('collapses negative / non-finite frames to 0 B (never NaN B or a negative transfer)', () => {
    expect(formatBytes(-1)).toBe('0 B');
    expect(formatBytes(NaN)).toBe('0 B');
    expect(formatBytes(Infinity)).toBe('0 B');
  });
});
