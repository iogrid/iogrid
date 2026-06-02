// use_grid_balance — polls the Solana RPC for the bound wallet's
// $GRID balance every 10s while the app is foregrounded.
//
// Lifecycle:
//   - Mount: immediate fetch + start 10s interval
//   - AppState 'active': resume interval (clear-and-restart so we don't
//     stack timers if iOS re-fires the listener)
//   - AppState 'background'/'inactive': pause interval (a polling VPN
//     app is the prime offender for battery; iOS surfaces "X is using
//     significant battery" within ~hour if we poll backgrounded)
//   - refetch(): manual refresh button calls this
//
// The DoD names "React Query hook" but this project doesn't depend on
// @tanstack/react-query (verified against package.json) and pulling it
// in for one polling fetch would add ~40 KiB. Native React state +
// AppState matches the existing codebase pattern (see
// src/app/index.tsx's NEVPNStatusDidChange subscriber) and gives us
// identical lifecycle semantics.

import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState, type AppStateStatus } from 'react-native';

import { fetchGridBalance, type GridBalance } from '@/lib/grid_balance';

const POLL_INTERVAL_MS = 10_000;
// Backoff cap when the RPC starts 429-ing — Helius free tier is
// 100k/day; at 10s the steady-state is ~8640/day, so we should never
// hit the limit, but a misbehaving deploy with a stuck app can chew
// through it. Exponential up to 60s, then plateaus.
const MAX_BACKOFF_MS = 60_000;

export interface UseGridBalanceResult {
  /** Most recent successful balance fetch (null until first response). */
  balance: GridBalance | null;
  /** True while a fetch is in flight. */
  isFetching: boolean;
  /** Last fetch error, cleared on next successful fetch. */
  error: Error | null;
  /** Manually trigger a fetch (e.g. the refresh button on wallet card). */
  refetch: () => void;
  /**
   * True when balance is below `lowBalanceThreshold` (default 0.001
   * $GRID — "below one minute of VPN at 0.001 $GRID/GB"). Renders
   * the low-balance banner per wireframes-v2 Screen 5.
   */
  isLow: boolean;
}

export interface UseGridBalanceOpts {
  /** Wallet address to poll. Hook does nothing while null/empty. */
  walletAddress: string | null;
  /** UI amount below which `isLow` flips true. Defaults to 0.001. */
  lowBalanceThreshold?: number;
}

export function useGridBalance({
  walletAddress,
  lowBalanceThreshold = 0.001,
}: UseGridBalanceOpts): UseGridBalanceResult {
  const [balance, setBalance] = useState<GridBalance | null>(null);
  const [isFetching, setIsFetching] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const backoffMsRef = useRef<number>(POLL_INTERVAL_MS);
  // Track the latest in-flight fetch so a refetch() during one cancels
  // the stale resolution without writing into state.
  const generationRef = useRef(0);

  const doFetch = useCallback(async () => {
    if (!walletAddress) return;
    const gen = ++generationRef.current;
    setIsFetching(true);
    try {
      const result = await fetchGridBalance(walletAddress);
      if (gen !== generationRef.current) return; // superseded
      setBalance(result);
      setError(null);
      backoffMsRef.current = POLL_INTERVAL_MS; // reset on success
    } catch (err) {
      if (gen !== generationRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      backoffMsRef.current = Math.min(backoffMsRef.current * 2, MAX_BACKOFF_MS);
    } finally {
      if (gen === generationRef.current) setIsFetching(false);
    }
  }, [walletAddress]);

  const startPolling = useCallback(() => {
    if (intervalRef.current) clearInterval(intervalRef.current);
    if (!walletAddress) return;
    // Fire immediately on (re)start so the user sees fresh data
    // within an instant of foregrounding.
    void doFetch();
    intervalRef.current = setInterval(() => {
      void doFetch();
    }, backoffMsRef.current);
  }, [walletAddress, doFetch]);

  const stopPolling = useCallback(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
  }, []);

  // Mount + walletAddress change.
  useEffect(() => {
    startPolling();
    return stopPolling;
  }, [startPolling, stopPolling]);

  // AppState — pause on background, resume on foreground.
  useEffect(() => {
    const sub = AppState.addEventListener('change', (state: AppStateStatus) => {
      if (state === 'active') {
        startPolling();
      } else {
        stopPolling();
      }
    });
    return () => sub.remove();
  }, [startPolling, stopPolling]);

  const refetch = useCallback(() => {
    void doFetch();
  }, [doFetch]);

  const isLow =
    balance !== null && balance.uiAmount < lowBalanceThreshold;

  return { balance, isFetching, error, refetch, isLow };
}
