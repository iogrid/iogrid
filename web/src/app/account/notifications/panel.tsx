"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import type {
  NotificationCategoryKey,
  NotificationChannelPrefs,
  NotificationPrefs,
} from "@/lib/types";
import {
  NOTIFICATION_CATEGORIES,
  defaultNotificationPrefs,
} from "@/lib/types";

/**
 * NotificationsPanel — per-user notification-channel preferences
 * (issue #631). Mirrors the load/save/toast pattern of the
 * /account/updates panel: GET on mount, POST on save, sonner toast on
 * success/error. Prefs persist server-side (identity-svc JSONB column),
 * never localStorage.
 */
export function NotificationsPanel() {
  const [saved, setSaved] = React.useState<NotificationPrefs>(
    defaultNotificationPrefs,
  );
  const [draft, setDraft] = React.useState<NotificationPrefs>(
    defaultNotificationPrefs,
  );
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    let cancelled = false;
    setLoading(true);
    browserApi()
      .get<{ prefs: NotificationPrefs | null }>(
        "/api/v1/account/notifications",
      )
      .then((r) => {
        if (cancelled) return;
        const merged = mergeWithDefaults(r.prefs);
        setSaved(merged);
        setDraft(merged);
      })
      .catch((e) => {
        if (!cancelled) toast.error((e as Error).message);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const dirty = React.useMemo(
    () => JSON.stringify(draft) !== JSON.stringify(saved),
    [draft, saved],
  );

  const setChannel = (
    key: NotificationCategoryKey,
    channel: keyof NotificationChannelPrefs,
    value: boolean,
  ) => {
    setDraft((d) => ({
      ...d,
      [key]: { ...d[key], [channel]: value },
    }));
  };

  const onSave = async () => {
    setSaving(true);
    try {
      await browserApi().post("/api/v1/account/notifications", {
        prefs: draft,
      });
      setSaved(draft);
      toast.success("Notification preferences saved.");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }

  return (
    <div className="space-y-6">
      <div className="rounded-md border border-border bg-card p-4 dark:border-border">
        <h2 className="text-base font-semibold">Notification channels</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Pick how you want to hear about each kind of event. Security
          alerts are recommended on every channel.
        </p>

        <div className="mt-4 overflow-hidden rounded-md border border-border dark:border-border">
          <div className="grid grid-cols-[1fr_5rem_5rem] items-center gap-2 border-b border-border bg-muted/40 px-4 py-2 text-xs font-medium text-muted-foreground dark:border-border">
            <span>Event</span>
            <span className="text-center">Email</span>
            <span className="text-center">In-app</span>
          </div>
          <ul className="divide-y divide-border dark:divide-border">
            {NOTIFICATION_CATEGORIES.map((cat) => (
              <li
                key={cat.key}
                className="grid grid-cols-[1fr_5rem_5rem] items-center gap-2 px-4 py-3 text-sm"
              >
                <div>
                  <div className="font-medium">{cat.label}</div>
                  <div className="mt-0.5 text-xs text-muted-foreground">
                    {cat.description}
                  </div>
                </div>
                <div className="flex justify-center">
                  <input
                    type="checkbox"
                    aria-label={`${cat.label} — email`}
                    checked={draft[cat.key].email}
                    onChange={(e) =>
                      setChannel(cat.key, "email", e.target.checked)
                    }
                  />
                </div>
                <div className="flex justify-center">
                  <input
                    type="checkbox"
                    aria-label={`${cat.label} — in-app`}
                    checked={draft[cat.key].in_app}
                    onChange={(e) =>
                      setChannel(cat.key, "in_app", e.target.checked)
                    }
                  />
                </div>
              </li>
            ))}
          </ul>
        </div>

        <div className="mt-4 flex gap-2">
          <Button
            size="sm"
            variant="default"
            disabled={!dirty || saving}
            onClick={onSave}
          >
            {saving ? "Saving…" : "Save"}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={!dirty || saving}
            onClick={() => setDraft(saved)}
          >
            Reset
          </Button>
        </div>
      </div>
    </div>
  );
}

// mergeWithDefaults fills any category/channel the server didn't store
// with the default, so a partial (or null) server object still renders
// every toggle in a defined state.
function mergeWithDefaults(
  prefs: NotificationPrefs | null | undefined,
): NotificationPrefs {
  const out = { ...defaultNotificationPrefs };
  if (!prefs) return out;
  for (const cat of NOTIFICATION_CATEGORIES) {
    const stored = prefs[cat.key];
    if (stored) {
      out[cat.key] = {
        email:
          typeof stored.email === "boolean"
            ? stored.email
            : out[cat.key].email,
        in_app:
          typeof stored.in_app === "boolean"
            ? stored.in_app
            : out[cat.key].in_app,
      };
    }
  }
  return out;
}
