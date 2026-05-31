"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";

// VPN-tagged API key. Reuses the same billing-svc ApiKeyService shape
// the existing /customer/api-keys page uses; we just filter to keys
// labelled "vpn:*" on the client so customers see the keys they minted
// specifically for VPN use.
type APIKey = {
  id: string;
  label: string;
  last_four: string;
  created_at: string;
  revoked: boolean;
  plaintext?: string; // only present immediately after mint
};

type ListAPIKeysResponse = { keys: APIKey[] };

// One active VPN session (from vpn-svc).
type VpnSession = {
  session_id: string;
  region: string;
  current_provider_id: string;
  state: string;
  bytes_in: number;
  bytes_out: number;
  created_at: string;
  last_activity_at: string;
};

type ListSessionsResponse = { sessions: VpnSession[]; count: number };

const VPN_KEY_PREFIX = "vpn:";

export function VpnPanel() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [keys, setKeys] = React.useState<APIKey[]>([]);
  const [sessions, setSessions] = React.useState<VpnSession[]>([]);
  const [loadingKeys, setLoadingKeys] = React.useState(true);
  const [loadingSessions, setLoadingSessions] = React.useState(true);
  const [creating, setCreating] = React.useState(false);
  const [newLabel, setNewLabel] = React.useState("My laptop");
  const [justCreated, setJustCreated] = React.useState<APIKey | null>(null);

  React.useEffect(() => {
    setWsId(localStorage.getItem("iogrid_workspace_id"));
  }, []);

  const refreshKeys = React.useCallback(async () => {
    if (!wsId) {
      setLoadingKeys(false);
      return;
    }
    try {
      const res = await browserApi().get<ListAPIKeysResponse>(
        `/api/v1/customer/api-keys?workspace_id=${wsId}`,
      );
      // Filter to VPN-tagged keys only — keep the /customer/api-keys
      // page clean for workload keys.
      setKeys((res.keys ?? []).filter((k) => k.label.startsWith(VPN_KEY_PREFIX)));
    } catch (e) {
      toast.error(`Failed to list keys: ${(e as Error).message}`);
    } finally {
      setLoadingKeys(false);
    }
  }, [wsId]);

  const refreshSessions = React.useCallback(async () => {
    if (!wsId) {
      setLoadingSessions(false);
      return;
    }
    try {
      const res = await browserApi().get<ListSessionsResponse>(
        `/api/v1/customer/vpn/sessions?workspace_id=${wsId}`,
      );
      setSessions(res.sessions ?? []);
    } catch (e) {
      // 404 = no sessions endpoint yet; degrade gracefully.
      setSessions([]);
    } finally {
      setLoadingSessions(false);
    }
  }, [wsId]);

  React.useEffect(() => {
    void refreshKeys();
    void refreshSessions();
    // Periodic refresh while the page is open so sessions count updates.
    const t = setInterval(refreshSessions, 15000);
    return () => clearInterval(t);
  }, [refreshKeys, refreshSessions]);

  if (!wsId) {
    return (
      <div className="rounded-md border border-warning/30 bg-warning/10 p-4 text-sm text-warning">
        Select a workspace on the{" "}
        <a href="/customer" className="underline">
          Overview tab
        </a>{" "}
        before managing VPN keys.
      </div>
    );
  }

  const onCreate = async () => {
    if (!newLabel.trim()) {
      toast.error("Label is required.");
      return;
    }
    setCreating(true);
    try {
      const key = await browserApi().post<APIKey>("/api/v1/customer/api-keys", {
        workspace_id: wsId,
        label: VPN_KEY_PREFIX + newLabel.trim(),
      });
      setJustCreated(key);
      setNewLabel("");
      void refreshKeys();
      toast.success("VPN key minted. Copy the plaintext now — it won't be shown again.");
    } catch (e) {
      toast.error(`Mint failed: ${(e as Error).message}`);
    } finally {
      setCreating(false);
    }
  };

  const onRevoke = async (id: string) => {
    if (!confirm("Revoke this key? Any device using it will immediately lose VPN access.")) return;
    try {
      await browserApi().del(`/api/v1/customer/api-keys/${id}?workspace_id=${wsId}`);
      toast.success("Key revoked.");
      void refreshKeys();
    } catch (e) {
      toast.error(`Revoke failed: ${(e as Error).message}`);
    }
  };

  const copy = (s: string) => {
    void navigator.clipboard.writeText(s);
    toast.success("Copied.");
  };

  return (
    <div className="space-y-8">
      {/* Active sessions */}
      <section className="rounded-md border border-border bg-card p-4">
        <h2 className="mb-3 text-lg font-semibold">Active sessions</h2>
        {loadingSessions ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : sessions.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No active sessions. Connect via the CLI:{" "}
            <code className="rounded bg-muted px-1.5 py-0.5">iogrid vpn connect --region us-east-1</code>
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="py-2">Region</th>
                <th className="py-2">Provider</th>
                <th className="py-2">State</th>
                <th className="py-2">In / Out</th>
                <th className="py-2">Last active</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.session_id} className="border-t border-border">
                  <td className="py-2">{s.region}</td>
                  <td className="py-2 font-mono text-xs">{s.current_provider_id.slice(0, 8)}</td>
                  <td className="py-2">{s.state}</td>
                  <td className="py-2 font-mono text-xs">
                    {humanBytes(s.bytes_in)} / {humanBytes(s.bytes_out)}
                  </td>
                  <td className="py-2 text-muted-foreground">{formatRelativeTime(s.last_activity_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* Mint */}
      <section className="rounded-md border border-border bg-card p-4">
        <h2 className="mb-3 text-lg font-semibold">VPN keys</h2>
        <div className="mb-4 flex items-end gap-2">
          <div className="flex-1">
            <label className="mb-1 block text-xs text-muted-foreground">Label (e.g. "My laptop", "Home server")</label>
            <Input
              value={newLabel}
              onChange={(e) => setNewLabel(e.target.value)}
              placeholder="My laptop"
              disabled={creating}
            />
          </div>
          <Button onClick={onCreate} disabled={creating}>
            {creating ? "Minting…" : "Mint VPN key"}
          </Button>
        </div>

        {justCreated?.plaintext && (
          <div className="mb-4 rounded-md border border-success/40 bg-success/10 p-3 text-sm">
            <p className="mb-2 font-medium text-success">Copy this key now — it will never be shown again:</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all rounded bg-background px-2 py-1.5 font-mono text-xs">
                {justCreated.plaintext}
              </code>
              <Button size="sm" variant="outline" onClick={() => copy(justCreated.plaintext!)}>
                Copy
              </Button>
            </div>
            <p className="mt-3 text-xs text-muted-foreground">
              Use it with the CLI: <code className="rounded bg-muted px-1 py-0.5">
                iogrid login --api-key=&lt;key&gt; --customer-id={wsId}
              </code>
            </p>
          </div>
        )}

        {loadingKeys ? (
          <p className="text-sm text-muted-foreground">Loading keys…</p>
        ) : keys.length === 0 ? (
          <p className="text-sm text-muted-foreground">No VPN keys yet. Mint one above.</p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="py-2">Label</th>
                <th className="py-2">Last 4</th>
                <th className="py-2">Created</th>
                <th className="py-2"></th>
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id} className="border-t border-border">
                  <td className="py-2">{k.label.replace(VPN_KEY_PREFIX, "")}</td>
                  <td className="py-2 font-mono text-xs">…{k.last_four}</td>
                  <td className="py-2 text-muted-foreground">{formatRelativeTime(k.created_at)}</td>
                  <td className="py-2 text-right">
                    {!k.revoked && (
                      <Button size="sm" variant="outline" onClick={() => onRevoke(k.id)}>
                        Revoke
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      {/* Install snippet */}
      <section className="rounded-md border border-border bg-card p-4">
        <h2 className="mb-3 text-lg font-semibold">Install the CLI</h2>
        <p className="mb-3 text-sm text-muted-foreground">
          One-line installer for macOS / Linux:
        </p>
        <div className="flex items-center gap-2">
          <code className="flex-1 break-all rounded bg-muted px-2 py-1.5 font-mono text-xs">
            curl -fsSL https://iogrid.org/install-cli.sh | sh
          </code>
          <Button size="sm" variant="outline" onClick={() => copy("curl -fsSL https://iogrid.org/install-cli.sh | sh")}>
            Copy
          </Button>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          Windows: download the .msi from{" "}
          <a href="https://releases.iogrid.org" className="underline">
            releases.iogrid.org
          </a>
          .
        </p>
      </section>
    </div>
  );
}

function humanBytes(n: number): string {
  if (!n || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}
