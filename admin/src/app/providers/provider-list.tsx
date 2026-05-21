"use client";

import * as React from "react";

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
 */
export function ProviderList() {
  const [providers, setProviders] = React.useState<Provider[] | null>(null);
  const [error, setError] = React.useState<string | null>(null);

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
            <th className="px-3 py-2 text-left font-medium">Registered</th>
            <th className="px-3 py-2 text-left font-medium">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {providers.map((p) => {
            const id = p.id?.value ?? "";
            const owner = p.ownerUserId?.value ?? "";
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
