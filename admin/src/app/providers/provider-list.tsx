"use client";

import * as React from "react";

import { formatMoney } from "@/lib/format";

type Provider = {
  id?: { value: string };
  ownerUserId?: { value: string };
  displayName?: string;
  status?: string;
  registeredAt?: string;
  lastSeenAt?: string;
  hostInfo?: { os?: string; arch?: string; hostname?: string };
};

type ListResp = { providers?: Provider[] };

// Money crosses the wire as proto3-JSON {currency, micros} where micros
// is the unit value × 1e6 (e.g. 11.05 $GRID == "11050000"). settledGrid
// is always $GRID; settledBuilds is the count of on-chain-settled iOS
// builds attributed to the provider.
type Money = { currency?: string; micros?: string };
type EarningsSummary = {
  settledGrid?: Money;
  settledBuilds?: string;
  totalEarned?: Money;
};
type EarningsResp = { summary?: EarningsSummary };

// Per-provider earnings cell state: undefined = not fetched yet,
// null = fetch failed (render "—"), else the parsed summary.
type EarningsCell = EarningsSummary | null | undefined;

/** micros → $GRID display string ("11050000" → "11.05 $GRID"). */
function gridFromMicros(m: Money | undefined): string {
  const micros = Number(m?.micros ?? 0);
  if (!Number.isFinite(micros) || micros === 0) return "0 $GRID";
  return formatMoney(micros / 1_000_000, "GRID");
}

/**
 * /providers list — fetches the registered-daemon roster from
 * the same-origin BFF proxy `/api/v1/providers/list` (#237, moved
 * out of web/src/app/api/v1/admin/providers/list/ in EPIC #422
 * Phase 1 — the entire admin/ app is admin-only, so `/admin` in
 * the URL would be redundant).
 *
 * The proxy reads the NextAuth session server-side, forwards to
 * providers-svc.ListProviders (Connect-RPC) with the IOGRID_SERVICE_TOKEN
 * + X-Iogrid-User-Id shim, and asserts the ADMIN role so the upstream
 * RequireRole check accepts the materialised Claims. Going same-origin
 * makes the NextAuth cookie ride the request and dodges the CORS
 * preflight that the previous cross-origin Connect-RPC call required.
 *
 * Earnings columns (#758): for each listed provider we additionally
 * fetch `/api/v1/admin/providers/{id}/earnings` (gateway-bff
 * GetAdminProviderEarnings → billing-svc.GetEarningsSummary, ADMIN-
 * gated, NOT ownership-gated) so the operator SEES the real settled
 * on-chain $GRID + build count for ANY provider — the founder's own
 * account owns a different daemon than the one that ran the builds, so
 * this is the only UI path to e.g. Hatice's 11.05 $GRID / 14 builds.
 */
export function ProviderList() {
  const [providers, setProviders] = React.useState<Provider[] | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  // providerId → settled-$GRID earnings (lazily fetched after the list).
  const [earnings, setEarnings] = React.useState<Record<string, EarningsCell>>(
    {},
  );

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/v1/providers/list", {
          method: "POST",
          credentials: "include",
          headers: { "content-type": "application/json" },
          body: "{}",
        });
        if (!res.ok) {
          setError(`${res.status} ${await res.text()}`);
          return;
        }
        const json: ListResp = await res.json();
        if (!cancelled) setProviders(json.providers ?? []);
      } catch (e) {
        if (!cancelled) setError(String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Once the roster is in, fan out one earnings fetch per provider. Each
  // resolves independently so a single failure renders "—" for that row
  // without blocking the others or the roster itself.
  React.useEffect(() => {
    if (!providers || providers.length === 0) return;
    let cancelled = false;
    for (const p of providers) {
      const id = p.id?.value;
      if (!id) continue;
      (async () => {
        try {
          const res = await fetch(
            `/api/v1/admin/providers/${encodeURIComponent(id)}/earnings`,
            { credentials: "include", headers: { accept: "application/json" } },
          );
          if (!res.ok) {
            if (!cancelled) setEarnings((e) => ({ ...e, [id]: null }));
            return;
          }
          const json: EarningsResp = await res.json();
          if (!cancelled)
            setEarnings((e) => ({ ...e, [id]: json.summary ?? {} }));
        } catch {
          if (!cancelled) setEarnings((e) => ({ ...e, [id]: null }));
        }
      })();
    }
    return () => {
      cancelled = true;
    };
  }, [providers]);

  if (error) {
    return (
      <div className="rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-800 dark:border-red-700 dark:bg-red-950 dark:text-red-200">
        Could not load providers: {error}
      </div>
    );
  }
  if (providers === null) {
    return (
      <div className="rounded-md border p-3 text-sm text-muted-foreground">
        Loading providers…
      </div>
    );
  }
  if (providers.length === 0) {
    return (
      <div className="rounded-md border p-3 text-sm text-muted-foreground">
        No providers paired yet. Have a user run{" "}
        <code>iogridd pair &lt;token&gt;</code> on their machine.
      </div>
    );
  }
  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-sm">
        <thead className="bg-muted/50">
          <tr>
            <th className="px-3 py-2 text-left font-medium">Provider</th>
            <th className="px-3 py-2 text-left font-medium">Owner</th>
            <th className="px-3 py-2 text-left font-medium">Status</th>
            <th className="px-3 py-2 text-right font-medium">Settled $GRID</th>
            <th className="px-3 py-2 text-right font-medium">Builds</th>
            <th className="px-3 py-2 text-left font-medium">Registered</th>
            <th className="px-3 py-2 text-left font-medium">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {providers.map((p) => {
            const id = p.id?.value ?? "";
            const owner = p.ownerUserId?.value ?? "";
            const e = earnings[id];
            return (
              <tr key={id} className="border-t">
                <td className="px-3 py-2 font-mono text-xs">
                  {p.displayName ?? id.slice(0, 8)}
                  <div className="text-[10px] text-muted-foreground">{id}</div>
                </td>
                <td className="px-3 py-2 font-mono text-xs">
                  {owner.slice(0, 8)}…
                </td>
                <td className="px-3 py-2 text-xs">
                  {(p.status ?? "")
                    .replace("PROVIDER_STATUS_", "")
                    .toLowerCase()}
                </td>
                <td className="px-3 py-2 text-right font-mono text-xs tabular-nums">
                  {e === undefined ? (
                    <span className="text-muted-foreground">…</span>
                  ) : e === null ? (
                    <span className="text-muted-foreground">—</span>
                  ) : (
                    gridFromMicros(e.settledGrid)
                  )}
                </td>
                <td className="px-3 py-2 text-right font-mono text-xs tabular-nums">
                  {e === undefined ? (
                    <span className="text-muted-foreground">…</span>
                  ) : e === null ? (
                    <span className="text-muted-foreground">—</span>
                  ) : (
                    Number(e.settledBuilds ?? 0)
                  )}
                </td>
                <td className="px-3 py-2 text-xs">
                  {p.registeredAt
                    ? new Date(p.registeredAt).toLocaleString()
                    : "—"}
                </td>
                <td className="px-3 py-2 text-xs">
                  {p.lastSeenAt ? new Date(p.lastSeenAt).toLocaleString() : "—"}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
