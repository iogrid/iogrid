// DEVNET integration test for the Ping approve-verification path — Refs #629.
//
// Exercises the REAL `verifyApprovalBestEffort()` (the same code the mobile
// app runs after a Ping success-bounce) against a REAL on-chain devnet
// transaction involving the $GRID mint
// (BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR, Token-2022, 9 decimals).
//
// Until a real $GRID mint existed on devnet, this RPC poll path could never
// be exercised end-to-end. It now can. This test proves the verification
// path returns 'confirmed' for a tx that actually landed.
//
// ── CI-SAFETY: this test is GUARDED. It only runs when BOTH:
//      GRID_DEVNET_SIG          = a real devnet tx signature to verify
//      EXPO_PUBLIC_SOLANA_RPC_URL (optional; defaults to api.devnet.solana.com)
//    are set. With no GRID_DEVNET_SIG it is `describe.skip`'d, so the normal
//    jest suite (and CI) never reach out to devnet or flake on RPC limits.
//
//    To run it manually:
//      GRID_DEVNET_SIG=4kCKLJME5gz1QDo7uR5YB447GZPmBQQY59GP8BS6Uv1J6UyFNZGnYMJwf1SwMoEJoxGi3bcaiHpfr6QS6hLCdX1T \
//      EXPO_PUBLIC_SOLANA_RPC_URL=https://api.devnet.solana.com \
//      npx jest --config jest.config.js ping-pay-devnet
//
//    Or via the convenience wrapper: solana/grid/verify-devnet.sh

import { verifyApprovalBestEffort } from '../ping-pay';

const DEVNET_SIG = process.env.GRID_DEVNET_SIG;

// describe.skip when no signature is provided so CI never depends on devnet.
const maybe = DEVNET_SIG ? describe : describe.skip;

maybe('verifyApprovalBestEffort — REAL devnet $GRID tx (Refs #629)', () => {
  // Real network polling needs more than the default 5s ts-jest budget.
  jest.setTimeout(30_000);

  beforeAll(() => {
    process.env.EXPO_PUBLIC_SOLANA_RPC_URL =
      process.env.EXPO_PUBLIC_SOLANA_RPC_URL ?? 'https://api.devnet.solana.com';
  });

  it('returns "confirmed" for a real on-chain $GRID transfer that landed', async () => {
    const status = await verifyApprovalBestEffort(DEVNET_SIG as string, {
      attempts: 6,
      intervalMs: 2000,
    });
    // The tx is final on devnet — the RPC getTransaction poll must see it
    // with meta.err === null and report 'confirmed'. This is the exact same
    // code path the app runs on a Ping success-bounce signature.
    expect(status).toBe('confirmed');
  });

  it('returns "unsupported" for a null signature (cancel-bounce shape)', async () => {
    // Cheap pure-logic assertion that needs no network — documents the
    // contract the app relies on when Ping returns ok=0 (no signature).
    expect(await verifyApprovalBestEffort(null)).toBe('unsupported');
  });

  it('returns "pending" for a well-formed but non-existent signature', async () => {
    // A 64-char base58-shaped signature that was never broadcast: the poll
    // exhausts its (short) budget and reports 'pending' (not 'confirmed').
    // Keep attempts/interval tiny so this stays fast.
    const bogus =
      '1111111111111111111111111111111111111111111111111111111111111111';
    const status = await verifyApprovalBestEffort(bogus, {
      attempts: 1,
      intervalMs: 0,
    });
    expect(status).toBe('pending');
  });
});
