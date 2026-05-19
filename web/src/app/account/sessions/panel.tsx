"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type { ListSessionsResponse, Session } from "@/lib/types";

export function SessionsPanel() {
  const [sessions, setSessions] = React.useState<Session[]>([]);
  const [loading, setLoading] = React.useState(true);

  const refresh = React.useCallback(() => {
    setLoading(true);
    browserApi()
      .get<ListSessionsResponse>("/api/v1/account/sessions")
      .then((r) => setSessions(r.sessions ?? []))
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const onRevoke = async (s: Session) => {
    try {
      await browserApi().post("/api/v1/account/sign-out", {
        refresh_token: s.id?.value ?? "",
      });
      toast.success("Session revoked.");
      refresh();
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  return (
    <div className="space-y-3">
      {loading ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : sessions.length === 0 ? (
        <p className="rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700">
          No active sessions besides this one.
        </p>
      ) : (
        <ul className="divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          {sessions.map((s) => (
            <li key={s.id?.value} className="flex items-center justify-between p-3 text-sm">
              <div className="min-w-0 flex-1">
                <p className="font-medium">
                  {s.userAgent || "Unknown device"}
                  {s.current ? (
                    <span className="ml-2 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
                      Current
                    </span>
                  ) : null}
                </p>
                <p className="text-xs text-zinc-500">
                  {s.ipAddress} · last seen {formatRelativeTime(s.lastSeenAt)}
                </p>
              </div>
              <Button
                size="sm"
                variant="outline"
                disabled={s.current}
                onClick={() => onRevoke(s)}
              >
                {s.current ? "—" : "Revoke"}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
