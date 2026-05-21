"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { ProviderRef } from "@/lib/types";

/**
 * PrimaryProviderPicker — multi-daemon ownership UI for /provide/schedule
 * (issue #325, family of #305).
 *
 * Rendered above the schedule form when the caller owns ≥2 paired
 * daemons. Hatice (manual-test + her real Mac) was the founder operator
 * who hit the wrong-daemon-selected bug on the EPIC #309 DoD walk: the
 * BFF used to pick "first ACTIVE" non-deterministically, so the editor
 * showed the wrong daemon's caps. The fix is two-fold:
 *
 *   1. Server: providers-svc now carries an explicit per-owner
 *      is_primary flag with at-most-one-true-per-owner; gateway-bff
 *      defaults to that row when no ?provider_id= is supplied.
 *   2. UI: this picker lets owners CHOOSE which daemon's schedule
 *      they're editing (the dropdown re-fetches with ?provider_id=X)
 *      and PROMOTE a non-primary row to primary (PUT
 *      /api/v1/provide/primary-provider).
 *
 * When `providers.length === 1` we deliberately render the read-only
 * "Editing schedule for: <name>" pill inline (no dropdown, no buttons)
 * — single-daemon owners never see the picker chrome.
 *
 * onSelect is invoked with the chosen provider_id; the parent uses it
 * to re-fetch /api/v1/provide/schedule?provider_id=<id>. onPromote is
 * invoked when the operator clicks "Set as primary"; the parent
 * triggers the PUT and refreshes the list afterwards.
 */
export interface PrimaryProviderPickerProps {
  providers: ProviderRef[];
  /** Currently-selected provider id (from selection or default-primary). */
  selectedId: string | null;
  /** Called when the operator picks a different daemon from the dropdown. */
  onSelect: (providerId: string) => void;
  /** Called when the operator clicks "Set as primary" next to a non-primary row. */
  onPromote: (providerId: string) => void;
  /** True while a promote PUT is in flight — disables every promote button. */
  promoting?: boolean;
  className?: string;
}

export function PrimaryProviderPicker({
  providers,
  selectedId,
  onSelect,
  onPromote,
  promoting,
  className,
}: PrimaryProviderPickerProps) {
  // Lookup helpers: id → display name; id → is_primary flag.
  const byId = React.useMemo(() => {
    const m = new Map<string, ProviderRef>();
    for (const p of providers) {
      const id = p.id?.value;
      if (id) m.set(id, p);
    }
    return m;
  }, [providers]);

  if (providers.length === 0) return null;

  // Resolve the currently-displayed name. Falls back to the first
  // provider's name when the parent hasn't picked one yet.
  const resolvedId = selectedId ?? providers[0]?.id?.value ?? "";
  const resolved = byId.get(resolvedId) ?? providers[0];
  const resolvedName = displayName(resolved);

  // Single-daemon owners get the read-only inline pill, NOT the picker.
  if (providers.length === 1) {
    return (
      <p
        data-testid="primary-provider-pill"
        className={cn(
          "text-sm text-muted-foreground dark:text-muted-foreground",
          className,
        )}
      >
        Editing schedule for{" "}
        <span className="font-medium text-foreground dark:text-foreground">
          {resolvedName}
        </span>
      </p>
    );
  }

  return (
    <section
      data-testid="primary-provider-picker"
      aria-labelledby="primary-provider-picker-heading"
      className={cn(
        "rounded-md border border-border bg-muted p-4 dark:border-border dark:bg-card",
        className,
      )}
    >
      <h3
        id="primary-provider-picker-heading"
        className="text-sm font-semibold text-foreground dark:text-foreground"
      >
        Editing schedule for
      </h3>
      <div className="mt-2 flex flex-wrap items-center gap-3">
        <label className="sr-only" htmlFor="primary-provider-select">
          Choose a daemon
        </label>
        <select
          id="primary-provider-select"
          data-testid="primary-provider-select"
          value={resolvedId}
          onChange={(e) => onSelect(e.target.value)}
          className="h-9 rounded-md border border-border-strong bg-background px-2 text-sm dark:border-border-strong"
        >
          {providers.map((p, i) => {
            const id = p.id?.value ?? `provider-${i}`;
            return (
              <option key={id} value={id}>
                {displayName(p)}
                {p.is_primary ? " (primary)" : ""}
              </option>
            );
          })}
        </select>
      </div>

      <ul
        data-testid="primary-provider-list"
        className="mt-4 space-y-2 text-sm"
      >
        {providers.map((p, i) => {
          const id = p.id?.value ?? `provider-${i}`;
          const isPrimary = !!p.is_primary;
          return (
            <li
              key={id}
              data-testid="primary-provider-row"
              className="flex items-center justify-between gap-3 rounded-md border border-border bg-background px-3 py-2 dark:border-border"
            >
              <span className="flex items-center gap-2">
                <span className="font-medium text-foreground dark:text-foreground">
                  {displayName(p)}
                </span>
                {isPrimary ? (
                  <span
                    data-testid="primary-provider-badge"
                    className="inline-flex items-center rounded-full bg-success/15 px-2 py-0.5 text-xs font-medium text-success dark:bg-success/15 dark:text-success"
                  >
                    Primary
                  </span>
                ) : null}
              </span>
              {isPrimary ? null : (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={promoting}
                  data-testid="primary-provider-promote"
                  onClick={() => onPromote(id)}
                >
                  Set as primary
                </Button>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// displayName picks the friendliest label for a provider. Falls back to
// a truncated id when display_name is empty — paired daemons that
// haven't sent a display_name still get a recognisable entry.
function displayName(p?: ProviderRef): string {
  const name = p?.display_name?.trim();
  if (name) return name;
  const id = p?.id?.value ?? "";
  return id ? `daemon ${id.slice(0, 8)}` : "Unnamed daemon";
}
