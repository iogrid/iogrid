"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type {
  UpdatePreferences,
  UpdateState,
  UpdateHistoryEntry,
} from "@/lib/types";

/**
 * UpdatesPanel — operator-facing controls for the daemon auto-update
 * worker (issue #59). Mirrors the JSON shape returned by the BFF's
 * GET /api/v1/account/updates endpoint.
 */
export function UpdatesPanel() {
  const [state, setState] = React.useState<UpdateState | null>(null);
  const [prefs, setPrefs] = React.useState<UpdatePreferences>({
    channel: "stable",
    autoUpdate: false,
  });
  const [loading, setLoading] = React.useState(true);
  const [checking, setChecking] = React.useState(false);

  const refresh = React.useCallback(() => {
    setLoading(true);
    browserApi()
      .get<{
        state: UpdateState;
        preferences: UpdatePreferences;
      }>("/api/v1/account/updates")
      .then((r) => {
        setState(r.state);
        setPrefs(r.preferences);
      })
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const onCheckNow = async () => {
    setChecking(true);
    try {
      await browserApi().post("/api/v1/account/updates/check", {});
      toast.success("Update check queued — refreshing in a moment.");
      // Daemon's check is async; small delay then refresh.
      window.setTimeout(refresh, 1500);
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setChecking(false);
    }
  };

  const onSavePrefs = async (next: UpdatePreferences) => {
    try {
      await browserApi().post("/api/v1/account/updates/preferences", next);
      setPrefs(next);
      toast.success("Preferences saved.");
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const onApplyPending = async () => {
    try {
      await browserApi().post("/api/v1/account/updates/apply", {});
      toast.success(
        "Apply requested — daemon will restart and the new version takes over.",
      );
      window.setTimeout(refresh, 2000);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const onRollback = async () => {
    if (
      !window.confirm(
        "Roll back to the previous daemon binary? The daemon will restart on the older version.",
      )
    ) {
      return;
    }
    try {
      await browserApi().post("/api/v1/account/updates/rollback", {});
      toast.success("Rollback queued.");
      window.setTimeout(refresh, 2000);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }

  const pending = state?.pendingVersion;

  return (
    <div className="space-y-6">
      <PreferencesCard
        prefs={prefs}
        onSave={onSavePrefs}
        onCheckNow={onCheckNow}
        checking={checking}
      />

      {pending ? (
        <PendingCard
          version={pending}
          onApply={onApplyPending}
          onRollback={onRollback}
        />
      ) : (
        <CurrentCard state={state} onRollback={onRollback} />
      )}

      <HistoryList items={state?.history ?? []} />
    </div>
  );
}

function PreferencesCard({
  prefs,
  onSave,
  onCheckNow,
  checking,
}: {
  prefs: UpdatePreferences;
  onSave: (p: UpdatePreferences) => Promise<void>;
  onCheckNow: () => Promise<void>;
  checking: boolean;
}) {
  const [draft, setDraft] = React.useState<UpdatePreferences>(prefs);
  React.useEffect(() => setDraft(prefs), [prefs]);

  const dirty = draft.channel !== prefs.channel || draft.autoUpdate !== prefs.autoUpdate;

  return (
    <div className="rounded-md border border-border bg-card p-4 dark:border-border">
      <h2 className="text-base font-semibold">Preferences</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        Pick a release channel and decide whether the daemon should update
        itself in the background.
      </p>

      <fieldset className="mt-4 space-y-2 text-sm">
        <legend className="font-medium">Release channel</legend>
        {(["stable", "beta", "canary"] as const).map((c) => (
          <label key={c} className="flex items-center gap-2">
            <input
              type="radio"
              name="channel"
              value={c}
              checked={draft.channel === c}
              onChange={() => setDraft((d) => ({ ...d, channel: c }))}
            />
            <span className="capitalize">{c}</span>
            <span className="text-xs text-muted-foreground">
              {c === "stable"
                ? "production releases (recommended)"
                : c === "beta"
                  ? "pre-release candidates"
                  : "bleeding edge — internal use only"}
            </span>
          </label>
        ))}
      </fieldset>

      <label className="mt-4 flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={draft.autoUpdate}
          onChange={(e) =>
            setDraft((d) => ({ ...d, autoUpdate: e.target.checked }))
          }
        />
        <span>
          <span className="font-medium">Install updates automatically</span>
          <span className="ml-2 text-xs text-muted-foreground">
            polls every 6h, applies on next idle window
          </span>
        </span>
      </label>

      <div className="mt-4 flex gap-2">
        <Button
          size="sm"
          variant="default"
          disabled={!dirty}
          onClick={() => onSave(draft)}
        >
          Save
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={onCheckNow}
          disabled={checking}
        >
          {checking ? "Checking…" : "Check now"}
        </Button>
      </div>
    </div>
  );
}

function PendingCard({
  version,
  onApply,
  onRollback,
}: {
  version: string;
  onApply: () => Promise<void>;
  onRollback: () => Promise<void>;
}) {
  return (
    <div className="rounded-md border border-warning/40 bg-warning/10 p-4 dark:border-warning/40 dark:bg-warning/15">
      <h2 className="text-base font-semibold">Pending: {version}</h2>
      <p className="mt-1 text-sm text-foreground dark:text-muted-foreground">
        A new daemon binary has been staged on disk. Apply it now to restart
        the daemon on the new version. If anything goes wrong within 30s the
        wrapper rolls back automatically.
      </p>
      <div className="mt-3 flex gap-2">
        <Button size="sm" variant="default" onClick={onApply}>
          Apply &amp; restart
        </Button>
        <Button size="sm" variant="outline" onClick={onRollback}>
          Discard
        </Button>
      </div>
    </div>
  );
}

function CurrentCard({
  state,
  onRollback,
}: {
  state: UpdateState | null;
  onRollback: () => Promise<void>;
}) {
  return (
    <div className="rounded-md border border-border bg-card p-4 dark:border-border">
      <h2 className="text-base font-semibold">Current</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {state?.enabled ? (
          <>Auto-update enabled — last check {lastCheckSummary(state)}.</>
        ) : (
          <>Auto-update is currently disabled. Enable it above to keep your daemon current.</>
        )}
      </p>
      <div className="mt-3">
        <Button size="sm" variant="outline" onClick={onRollback}>
          Roll back to previous binary
        </Button>
      </div>
    </div>
  );
}

function lastCheckSummary(state: UpdateState): string {
  const e = state.history?.[0];
  if (!e) return "never";
  return formatRelativeTime(e.at);
}

function HistoryList({ items }: { items: UpdateHistoryEntry[] }) {
  if (!items?.length) {
    return (
      <div className="rounded-md border border-dashed border-border-strong p-4 text-center text-sm text-muted-foreground dark:border-border-strong">
        No update checks yet.
      </div>
    );
  }
  return (
    <div className="rounded-md border border-border dark:border-border">
      <h2 className="border-b border-border px-4 py-2 text-sm font-semibold dark:border-border">
        History
      </h2>
      <ul className="divide-y divide-border dark:divide-border">
        {items.map((h, i) => (
          <li key={`${h.at}-${i}`} className="px-4 py-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="font-mono text-xs text-muted-foreground">
                {formatRelativeTime(h.at)}
              </span>
              <OutcomeBadge outcome={h.outcome} />
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              channel {h.channel} · running {h.fromVersion}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function OutcomeBadge({ outcome }: { outcome: UpdateHistoryEntry["outcome"] }) {
  const status = outcome.status;
  const text =
    status === "up_to_date"
      ? "Up to date"
      : status === "staged"
        ? `Staged ${outcome.to}`
        : status === "skipped"
          ? `Skipped: ${outcome.reason}`
          : status === "failed"
            ? `Failed: ${outcome.error}`
            : status;
  const tone =
    status === "up_to_date"
      ? "bg-success/15 text-success dark:bg-success/15 dark:text-success"
      : status === "staged"
        ? "bg-warning/15 text-warning dark:bg-warning/15 dark:text-warning"
        : status === "failed"
          ? "bg-destructive/15 text-destructive dark:bg-destructive/15 dark:text-destructive"
          : "bg-muted text-foreground dark:bg-muted dark:text-muted-foreground";
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${tone}`}
    >
      {text}
    </span>
  );
}
