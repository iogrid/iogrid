"use client";

import * as React from "react";
import { browserApi } from "@/lib/api";
import type { ProviderDashboard } from "@/lib/types";

/**
 * useProviderOwnership — single source of truth for the "does the
 * caller own at least one paired provider?" gate that the /provider/*
 * surfaces use to decide between the real dashboard and the "Install
 * daemon" empty-state (issue #313).
 *
 * Backed by GET /api/v1/provide/dashboard. The gateway-bff guarantees
 * a fast empty envelope when ownership is zero (it short-circuits
 * before any providers-svc fan-out), so calling this from every
 * sub-page is cheap.
 *
 * The hook DELIBERATELY returns `hasProvider: null` while loading so
 * callers can render their own neutral skeleton without prematurely
 * deciding which branch to show. Once the request resolves we surface
 * a strict boolean.
 *
 * Errors are non-fatal: if the dashboard probe fails (e.g. network
 * blip, BFF roll-out), we default to `hasProvider: true` so the
 * caller's existing data fetch + its own error path takes over. We do
 * NOT block real dashboards behind a probe failure — that would be a
 * §3.3 defensive-coding-masks-upstream-bug smell.
 */
export interface ProviderOwnershipState {
  hasProvider: boolean | null;
  loading: boolean;
  error: string | null;
}

export function useProviderOwnership(): ProviderOwnershipState {
  const [state, setState] = React.useState<ProviderOwnershipState>({
    hasProvider: null,
    loading: true,
    error: null,
  });

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await browserApi().get<ProviderDashboard>(
          "/api/v1/provide/dashboard",
        );
        if (cancelled) return;
        setState({
          // Treat anything other than an explicit `false` as "has provider"
          // — covers older BFF builds that didn't emit the flag yet
          // (graceful forward-compat with #310).
          hasProvider: res.has_provider === false ? false : true,
          loading: false,
          error: null,
        });
      } catch (e) {
        if (cancelled) return;
        setState({
          hasProvider: true,
          loading: false,
          error: (e as Error).message,
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}
