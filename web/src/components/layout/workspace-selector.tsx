"use client";

import * as React from "react";

/**
 * WorkspaceSelector is the dropdown that lets a signed-in user switch
 * between workspaces they belong to. Lives in the PortalShell header
 * (provider + customer dashboards) so workspace context is always
 * visible.
 *
 * For issue #146 this is a light-touch scaffold:
 *  - fetches GET /api/v1/workspaces on mount
 *  - persists the selected workspace_id in localStorage under
 *    "iogrid.activeWorkspaceId"
 *  - emits a "iogrid:workspaceChanged" CustomEvent so the rest of the
 *    UI can refetch on switch
 *
 * Full feature work (invite UI, billing tab, role badges) is tracked
 * downstream.
 */

export interface Workspace {
  id: string;
  name: string;
  plan: string;
  owner_user_id: string;
  caller_role?: string;
}

const STORAGE_KEY = "iogrid.activeWorkspaceId";
const SWITCH_EVENT = "iogrid:workspaceChanged";

export function readActiveWorkspaceId(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(STORAGE_KEY);
}

export function writeActiveWorkspaceId(id: string) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, id);
  window.dispatchEvent(
    new CustomEvent(SWITCH_EVENT, { detail: { workspaceId: id } }),
  );
}

export interface WorkspaceSelectorProps {
  /** Override the upstream URL for tests / Storybook. */
  endpoint?: string;
}

export function WorkspaceSelector({
  endpoint = "/api/v1/workspaces",
}: WorkspaceSelectorProps) {
  const [workspaces, setWorkspaces] = React.useState<Workspace[]>([]);
  const [activeId, setActiveId] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const res = await fetch(endpoint, { credentials: "include" });
        if (!res.ok) {
          throw new Error(`workspaces fetch failed: ${res.status}`);
        }
        const body: { workspaces?: Workspace[] } = await res.json();
        if (cancelled) return;
        const list = body.workspaces ?? [];
        setWorkspaces(list);
        const stored = readActiveWorkspaceId();
        const initial =
          stored && list.some((w) => w.id === stored)
            ? stored
            : list[0]?.id ?? null;
        if (initial) {
          setActiveId(initial);
          writeActiveWorkspaceId(initial);
        }
      } catch (e: unknown) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : "unknown error");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [endpoint]);

  const handleSelect = React.useCallback(
    (id: string) => {
      setActiveId(id);
      writeActiveWorkspaceId(id);
    },
    [setActiveId],
  );

  if (loading) {
    return (
      <span
        className="text-xs font-medium text-muted-foreground dark:text-muted-foreground"
        aria-live="polite"
      >
        Loading workspaces…
      </span>
    );
  }
  if (error) {
    return (
      <span
        className="text-xs font-medium text-destructive dark:text-destructive"
        role="alert"
      >
        Workspaces unavailable
      </span>
    );
  }
  if (workspaces.length === 0) {
    return (
      <span className="text-xs font-medium text-muted-foreground dark:text-muted-foreground">
        No workspace
      </span>
    );
  }

  return (
    <label className="flex items-center gap-2 text-xs font-medium text-foreground dark:text-foreground">
      <span className="hidden md:inline">Workspace</span>
      <select
        aria-label="Active workspace"
        value={activeId ?? ""}
        onChange={(e) => handleSelect(e.target.value)}
        className="rounded-md border border-border bg-background px-2 py-1 text-xs text-foreground transition-colors hover:border-border-strong focus:border-ring focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {workspaces.map((w) => (
          <option key={w.id} value={w.id}>
            {w.name}
            {w.caller_role ? ` · ${w.caller_role}` : ""}
          </option>
        ))}
      </select>
    </label>
  );
}
