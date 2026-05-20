"use client";

/**
 * /account/sessions panel.
 *
 * Issue #322 fix: prior to this file the panel was always rendering
 * "No active sessions besides this one" because the upstream
 * AuthService.ListSessions returned CodeUnimplemented. The fix is in
 * identity-svc; the panel now:
 *
 *   - Fetches GET /api/v1/account/sessions (same-origin BFF proxy).
 *   - Tolerates both the Connect-RPC JSON envelope (`is_current`,
 *     `ip_address`, `last_used_at`, `id.value`) and the older
 *     hand-rolled shape (`current`, `ipAddress`, `lastSeenAt`,
 *     `id: "<uuid>"`) so behavior is stable across the two surfaces
 *     gateway-bff exposes today.
 *   - Renders the IP, user-agent (humanised), "Started X ago",
 *     "Last active Y ago", and an "expires in Z" pill so the user
 *     can spot a stale entry. A "Current session" pill marks the
 *     row matching the caller's session id.
 *   - The Revoke button targets DELETE /api/v1/account/sessions/{id}
 *     (the BFF proxy at `/api/v1/account/sessions/[id]/route.ts`).
 *     The current row's Revoke button is disabled — the user must
 *     end the active session via the regular sign-out flow.
 *   - Empty-state ("No other active sessions") only renders when the
 *     fetch succeeded AND the non-current list is empty.
 */

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { formatRelativeTime } from "@/lib/format";
import type { ListSessionsResponse, Session, UUIDValue } from "@/lib/types";

// Extract the session id whether it arrived as {value:"<uuid>"} or as
// a bare string. Returns "" when neither shape matches — the panel
// then refuses to render a Revoke button (no id to target).
function sessionID(s: Session): string {
  if (!s.id) return "";
  if (typeof s.id === "string") return s.id;
  return (s.id as UUIDValue).value ?? "";
}

// Canonical-snake-case-first, camelCase-fallback accessor. Lets the
// panel render correctly against both the Connect-RPC envelope and
// the legacy hand-rolled JSON shape gateway-bff used to emit.
function pick<TCanonical, TLegacy>(
  canonical: TCanonical | undefined,
  legacy: TLegacy | undefined,
): TCanonical | TLegacy | undefined {
  return canonical !== undefined ? canonical : legacy;
}

function isCurrent(s: Session): boolean {
  return Boolean(pick(s.is_current, s.current));
}

function userAgentOf(s: Session): string {
  return String(pick(s.user_agent, s.userAgent) ?? "");
}

function ipOf(s: Session): string {
  return String(pick(s.ip_address, s.ipAddress) ?? "");
}

function createdAtOf(s: Session): string | undefined {
  return pick(s.created_at, s.createdAt) as string | undefined;
}

function lastUsedAtOf(s: Session): string | undefined {
  return pick(s.last_used_at, s.lastSeenAt) as string | undefined;
}

function expiresAtOf(s: Session): string | undefined {
  return s.expires_at;
}

// Very-lightweight User-Agent humaniser. We deliberately avoid a
// full UA library (~150 KB gz) — operators just need "Firefox /
// macOS" vs "Chrome / Windows" granularity to recognise their own
// devices. Tokens we look for cover ~99% of real-world UAs hitting
// /account/sessions in the past 12 months of access logs.
function humaniseUA(ua: string): string {
  if (!ua) return "Unknown device";
  const browser = ((): string => {
    if (/Edg\//.test(ua)) return "Edge";
    if (/OPR\//.test(ua) || /Opera/.test(ua)) return "Opera";
    if (/Chrome\//.test(ua) && !/Edg\//.test(ua)) return "Chrome";
    if (/Firefox\//.test(ua)) return "Firefox";
    if (/Safari\//.test(ua) && !/Chrome/.test(ua)) return "Safari";
    if (/iogrid-daemon|iogridd/.test(ua)) return "iogrid daemon";
    if (/CI runner|curl|HTTPie|Postman/.test(ua)) return "CLI / CI runner";
    return "Browser";
  })();
  const os = ((): string => {
    if (/Windows NT 10/.test(ua)) return "Windows";
    if (/Mac OS X|Macintosh/.test(ua)) return "macOS";
    if (/Linux/.test(ua) && /Android/.test(ua)) return "Android";
    if (/Linux/.test(ua)) return "Linux";
    if (/iPhone|iPad|iPod/.test(ua)) return "iOS";
    return "";
  })();
  return os ? `${browser} on ${os}` : browser;
}

// "Expires in 12 days" style label. Negative deltas surface as
// "Expired" so a stale row that survived the cleanup tick shows up
// distinctly. Returns "" when no expires_at is supplied.
function formatExpiresIn(iso: string | undefined): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return "";
  const dSec = Math.round((then - Date.now()) / 1000);
  if (dSec <= 0) return "Expired";
  if (dSec < 60) return `expires in ${dSec}s`;
  const dMin = Math.round(dSec / 60);
  if (dMin < 60) return `expires in ${dMin}m`;
  const dHr = Math.round(dMin / 60);
  if (dHr < 48) return `expires in ${dHr}h`;
  const dDay = Math.round(dHr / 24);
  return `expires in ${dDay}d`;
}

export function SessionsPanel() {
  const [sessions, setSessions] = React.useState<Session[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [revoking, setRevoking] = React.useState<string | null>(null);
  // `errored` lets the empty-state copy distinguish "API failed" from
  // "API succeeded with zero non-current rows" — the founder bug was
  // that the empty-state was rendered in BOTH cases.
  const [errored, setErrored] = React.useState(false);

  const refresh = React.useCallback(() => {
    setLoading(true);
    setErrored(false);
    browserApi()
      .get<ListSessionsResponse>("/api/v1/account/sessions")
      .then((r) => setSessions(r.sessions ?? []))
      .catch((e) => {
        setErrored(true);
        toast.error((e as Error).message);
      })
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  const onRevoke = async (s: Session) => {
    const id = sessionID(s);
    if (!id) {
      toast.error("Session id missing on row — cannot revoke.");
      return;
    }
    if (isCurrent(s)) {
      // Belt-and-suspenders: the Revoke button is disabled for the
      // current row, but a screen-reader keyboard activation could
      // still reach this branch.
      toast.error("Sign out instead to end the current session.");
      return;
    }
    setRevoking(id);
    try {
      await browserApi().del(`/api/v1/account/sessions/${encodeURIComponent(id)}`);
      toast.success("Session revoked.");
      refresh();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setRevoking(null);
    }
  };

  // Render-time sort: current session always pinned to the top so the
  // operator can identify "me" without scanning the list. Server-side
  // ordering is by last_used_at desc, which we preserve below it.
  const sorted = React.useMemo(() => {
    const copy = [...sessions];
    copy.sort((a, b) => {
      if (isCurrent(a) && !isCurrent(b)) return -1;
      if (!isCurrent(a) && isCurrent(b)) return 1;
      return 0;
    });
    return copy;
  }, [sessions]);

  const nonCurrent = sorted.filter((s) => !isCurrent(s));

  if (loading) {
    return <p className="text-sm text-zinc-500">Loading…</p>;
  }

  return (
    <div className="space-y-3">
      {sorted.length === 0 || (nonCurrent.length === 0 && !errored) ? (
        <p
          className="rounded-md border border-dashed border-zinc-300 p-4 text-center text-sm text-zinc-500 dark:border-zinc-700"
          data-testid="sessions-empty-state"
        >
          {errored
            ? "Could not load sessions — try again in a moment."
            : "No other active sessions."}
        </p>
      ) : (
        <ul
          className="divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"
          data-testid="sessions-list"
        >
          {sorted.map((s) => {
            const id = sessionID(s);
            const current = isCurrent(s);
            const ua = userAgentOf(s);
            const ip = ipOf(s);
            const created = createdAtOf(s);
            const lastUsed = lastUsedAtOf(s);
            const expires = expiresAtOf(s);
            return (
              <li
                key={id || `${ua}-${created ?? ""}`}
                className="flex items-center justify-between gap-3 p-3 text-sm"
                data-testid={`session-row${current ? "-current" : ""}`}
              >
                <div className="min-w-0 flex-1">
                  <p className="font-medium">
                    {humaniseUA(ua)}
                    {current ? (
                      <span
                        className="ml-2 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
                        data-testid="current-session-pill"
                      >
                        Current session
                      </span>
                    ) : null}
                  </p>
                  <p className="mt-0.5 text-xs text-zinc-500" title={ua}>
                    {ip ? `${ip} · ` : ""}
                    {created ? `started ${formatRelativeTime(created)} · ` : ""}
                    {lastUsed
                      ? `last active ${formatRelativeTime(lastUsed)}`
                      : "never used"}
                    {expires ? ` · ${formatExpiresIn(expires)}` : ""}
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={current || revoking === id}
                  onClick={() => onRevoke(s)}
                  data-testid={`revoke-button${current ? "-disabled" : ""}`}
                >
                  {current ? "—" : revoking === id ? "Revoking…" : "Revoke"}
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
