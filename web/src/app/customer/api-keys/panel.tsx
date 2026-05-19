"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type { APIKey, ListAPIKeysResponse } from "@/lib/types";

export function ApiKeysPanel() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [keys, setKeys] = React.useState<APIKey[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [creating, setCreating] = React.useState(false);
  const [newLabel, setNewLabel] = React.useState("");
  const [justCreated, setJustCreated] = React.useState<APIKey | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = React.useState<string | null>(null);

  React.useEffect(() => {
    setWsId(localStorage.getItem("iogrid_workspace_id"));
  }, []);

  const refresh = React.useCallback(async () => {
    if (!wsId) {
      setLoading(false);
      return;
    }
    try {
      const res = await browserApi().get<ListAPIKeysResponse>(
        `/api/v1/customer/api-keys?workspace_id=${wsId}`,
      );
      setKeys(res.keys ?? []);
    } catch (e) {
      toast.error(`Failed to list keys: ${(e as Error).message}`);
    } finally {
      setLoading(false);
    }
  }, [wsId]);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  if (!wsId) {
    return (
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">
        Select a workspace on the{" "}
        <a href="/customer" className="underline">
          Overview tab
        </a>{" "}
        before managing API keys.
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
      const key = await browserApi().post<APIKey>(
        "/api/v1/customer/api-keys",
        {
          workspace_id: wsId,
          label: newLabel.trim(),
        },
      );
      setJustCreated(key);
      setNewLabel("");
      void refresh();
      toast.success("API key created. Copy the plaintext now — it won't be shown again.");
    } catch (e) {
      toast.error(`Create failed: ${(e as Error).message}`);
    } finally {
      setCreating(false);
    }
  };

  const onDelete = async (id: string) => {
    try {
      await browserApi().del(
        `/api/v1/customer/api-keys/${id}?workspace_id=${wsId}`,
      );
      toast.success("Key revoked.");
      setConfirmDeleteId(null);
      void refresh();
    } catch (e) {
      toast.error(`Revoke failed: ${(e as Error).message}`);
    }
  };

  return (
    <div className="space-y-6">
      <section className="rounded-md border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
        <h2 className="text-sm font-medium">Create a key</h2>
        <p className="mt-1 text-xs text-zinc-500">
          Pick a label that reminds you what the key is for (e.g.
          &ldquo;production-staging-runner&rdquo;). The plaintext token is shown only once.
        </p>
        <form
          data-testid="create-key-form"
          onSubmit={(e) => {
            e.preventDefault();
            void onCreate();
          }}
          className="mt-3 flex gap-2"
        >
          <Input
            type="text"
            value={newLabel}
            onChange={(e) => setNewLabel(e.target.value)}
            placeholder="ci-runner-prod"
            aria-label="Label"
            required
          />
          <Button type="submit" disabled={creating}>
            {creating ? "Creating…" : "Create key"}
          </Button>
        </form>
      </section>

      {justCreated ? (
        <PlaintextReveal apiKey={justCreated} onDismiss={() => setJustCreated(null)} />
      ) : null}

      <section>
        <h2 className="text-sm font-medium">Active keys</h2>
        {loading ? (
          <p className="mt-3 text-sm text-zinc-500">Loading…</p>
        ) : keys.length === 0 ? (
          <p className="mt-3 rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700">
            No keys yet. Create one above.
          </p>
        ) : (
          <ul
            data-testid="api-keys-list"
            className="mt-3 divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"
          >
            {keys.map((k) => {
              const id = k.id?.value ?? "";
              const isConfirming = confirmDeleteId === id;
              return (
                <li key={id} className="flex items-center justify-between p-3 text-sm">
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">{k.label || "(unnamed)"}</p>
                    <p className="text-xs text-zinc-500">
                      <code className="font-mono">{k.prefix}…</code> · created{" "}
                      {formatRelativeTime(k.createdAt)}
                    </p>
                  </div>
                  {isConfirming ? (
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setConfirmDeleteId(null)}
                      >
                        Cancel
                      </Button>
                      <Button
                        size="sm"
                        variant="default"
                        onClick={() => void onDelete(id)}
                        className="bg-rose-600 hover:bg-rose-500"
                      >
                        Revoke permanently
                      </Button>
                    </div>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => setConfirmDeleteId(id)}
                      aria-label={`Revoke ${k.label}`}
                    >
                      Revoke
                    </Button>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}

function PlaintextReveal({
  apiKey,
  onDismiss,
}: {
  apiKey: APIKey;
  onDismiss: () => void;
}) {
  const [copied, setCopied] = React.useState(false);
  const onCopy = async () => {
    if (!apiKey.plaintext) return;
    try {
      await navigator.clipboard.writeText(apiKey.plaintext);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("Copy failed — select the field and use Ctrl+C.");
    }
  };
  return (
    <div
      role="alertdialog"
      aria-labelledby="key-reveal-title"
      data-testid="plaintext-reveal"
      className="rounded-md border border-emerald-400 bg-emerald-50 p-4 text-sm dark:border-emerald-700 dark:bg-emerald-950"
    >
      <p id="key-reveal-title" className="font-medium">
        Copy this token now
      </p>
      <p className="mt-1 text-xs text-zinc-700 dark:text-zinc-300">
        Anyone with this token can submit workloads under your workspace.
        Treat it like a password — iogrid will never show it to you again.
      </p>
      <div className="mt-3 flex gap-2">
        <input
          readOnly
          aria-label="Plaintext API key"
          value={apiKey.plaintext ?? ""}
          className="flex-1 rounded-md border border-zinc-300 bg-white px-2 py-1.5 font-mono text-xs dark:border-zinc-700 dark:bg-zinc-900"
        />
        <Button size="sm" onClick={onCopy}>
          {copied ? "Copied!" : "Copy"}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDismiss}>
          Dismiss
        </Button>
      </div>
    </div>
  );
}
