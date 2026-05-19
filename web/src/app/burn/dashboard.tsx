"use client";

import * as React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { BurnTimeseries } from "@/components/wallet/BurnTimeseries";
import { browserApi } from "@/lib/api";
import { formatToken } from "@/lib/solana/balances";
import { formatRelativeTime } from "@/lib/format";
import {
  fetchBurnDaily,
  fetchBurnEvents,
  fetchBurnSummary,
  type BurnDailyPoint,
  type BurnEvent,
  type BurnSummary,
} from "@/lib/solana/burn";

interface BurnState {
  summary: BurnSummary | null;
  daily: BurnDailyPoint[];
  events: BurnEvent[];
  loading: boolean;
  error: string | null;
}

export function BurnDashboard() {
  const [state, setState] = React.useState<BurnState>({
    summary: null,
    daily: [],
    events: [],
    loading: true,
    error: null,
  });

  React.useEffect(() => {
    let cancelled = false;
    const load = async () => {
      const client = browserApi();
      try {
        const [summary, daily, events] = await Promise.all([
          fetchBurnSummary(client),
          fetchBurnDaily(client, 30),
          fetchBurnEvents(client, 20),
        ]);
        if (!cancelled) {
          setState({
            summary,
            daily: daily.points ?? [],
            events: events.events ?? [],
            loading: false,
            error: null,
          });
        }
      } catch (e) {
        if (!cancelled) {
          setState((s) => ({
            ...s,
            loading: false,
            error: (e as Error).message,
          }));
        }
      }
    };
    void load();
    const id = setInterval(load, 60_000); // refresh once per minute
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  return (
    <div className="space-y-6" data-testid="burn-dashboard">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle>Total burned (cumulative)</CardTitle>
          </CardHeader>
          <CardContent>
            <p
              className="text-3xl font-semibold tabular-nums"
              data-testid="burn-total"
            >
              {state.summary ? formatToken(state.summary.totalBurnedUi, 0) : "—"}
            </p>
            <p className="mt-1 text-xs text-zinc-500">$GRID permanently removed</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Last burn</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-3xl font-semibold">
              {state.summary?.lastBurnAt
                ? formatRelativeTime(state.summary.lastBurnAt)
                : "—"}
            </p>
            <p className="mt-1 text-xs text-zinc-500">on-chain timestamp</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>30-day rolling</CardTitle>
          </CardHeader>
          <CardContent>
            <p
              className="text-3xl font-semibold tabular-nums"
              data-testid="burn-rolling-30d"
            >
              {formatToken(
                state.daily.reduce((acc, d) => acc + d.burnedUi, 0),
                0,
              )}
            </p>
            <p className="mt-1 text-xs text-zinc-500">$GRID burned</p>
          </CardContent>
        </Card>
      </div>

      {state.error ? (
        <p className="text-sm text-rose-600" data-testid="burn-error">
          Couldn&apos;t load the burn ledger: {state.error}
        </p>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Daily burns — last 30 days</CardTitle>
        </CardHeader>
        <CardContent>
          <BurnTimeseries data={state.daily} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Recent burn events</CardTitle>
        </CardHeader>
        <CardContent>
          {state.events.length === 0 ? (
            <p
              className="rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700"
              data-testid="burn-events-empty"
            >
              {state.loading ? "Loading…" : "No burn events yet."}
            </p>
          ) : (
            <ul className="space-y-2" data-testid="burn-events-list">
              {state.events.map((ev) => (
                <li
                  key={ev.id}
                  className="flex items-center justify-between gap-3 rounded-md border border-zinc-100 bg-zinc-50 px-3 py-2 text-sm dark:border-zinc-800 dark:bg-zinc-900"
                >
                  <div>
                    <p className="font-mono tabular-nums">
                      −{formatToken(ev.amountUi)} $GRID
                    </p>
                    <p className="text-xs text-zinc-500">
                      {formatRelativeTime(ev.timestamp)}
                      {ev.source ? ` · ${ev.source}` : ""}
                    </p>
                  </div>
                  <a
                    href={`https://solscan.io/tx/${ev.txSignature}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-mono text-xs text-zinc-500 underline hover:text-zinc-900 dark:hover:text-zinc-100"
                  >
                    {ev.txSignature.slice(0, 8)}…
                  </a>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
