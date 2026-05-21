"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ApiError, browserApi } from "@/lib/api";
import { identifierKindLabel } from "@/lib/proto-enum";
import type { MeResponse, Identifier } from "@/lib/types";

export function IdentifiersPanel() {
  const [me, setMe] = React.useState<MeResponse | null>(null);
  const [loading, setLoading] = React.useState(true);

  const refresh = React.useCallback(() => {
    setLoading(true);
    browserApi()
      .get<MeResponse>("/api/v1/me")
      .then(setMe)
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, []);

  React.useEffect(() => {
    refresh();
  }, [refresh]);

  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading identifiers…
      </div>
    );
  }
  if (!me) {
    return (
      <div className="rounded-md border border-rose-200 bg-rose-50 p-4 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950 dark:text-rose-300">
        Couldn&apos;t load identifiers. Try refreshing or sign back in.
      </div>
    );
  }

  // Optimistic-ish: drop the row from local state on success; on failure
  // toast and refetch the canonical list so we never show a stale view.
  const onRemoved = (identifierID: string) => {
    setMe((prev) =>
      prev
        ? {
            ...prev,
            identifiers: (prev.identifiers ?? []).filter(
              (i) => (i.id?.value ?? "") !== identifierID,
            ),
          }
        : prev,
    );
  };

  return (
    <div className="space-y-4">
      <ul className="divide-y divide-zinc-200 rounded-md border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        {(me.identifiers ?? []).map((id) => (
          <IdentifierRow
            key={id.id?.value ?? id.value}
            id={id}
            onRemoved={onRemoved}
            onError={refresh}
          />
        ))}
        {(me.identifiers ?? []).length === 0 ? (
          <li className="p-4 text-sm text-zinc-500">
            No identifiers bound. Add one to keep the account recoverable.
          </li>
        ) : null}
      </ul>
      <p className="text-xs text-zinc-500">
        Adding a new identifier opens a verification flow in the identity
        service. Removing the last verified identifier is blocked server-side.
      </p>
    </div>
  );
}

function IdentifierRow({
  id,
  onRemoved,
  onError,
}: {
  id: Identifier;
  onRemoved: (identifierID: string) => void;
  onError: () => void;
}) {
  const [pending, setPending] = React.useState(false);

  const onRemove = async () => {
    const identifierID = id.id?.value;
    if (!identifierID) {
      toast.error("Missing identifier id — refresh and try again.");
      return;
    }
    setPending(true);
    try {
      await browserApi().del(`/api/v1/me/identifiers/${identifierID}`);
      onRemoved(identifierID);
      toast.success("Identifier removed.");
    } catch (e) {
      if (e instanceof ApiError && e.code === "last_identifier") {
        toast.error(
          "This is your last verified identifier — add another before removing it.",
        );
      } else {
        toast.error((e as Error).message);
      }
      // Re-sync from server so we don't end up out of step on a partial
      // failure (network drop, transaction rollback).
      onError();
    } finally {
      setPending(false);
    }
  };

  // Wire shape (gateway-bff via stdlib encoding/json): the displayable
  // value lives in `verified_email` for magic-link, OR `subject` for
  // OAuth identifiers. The legacy `value` field is the pre-#371
  // camelCase fallback. Verified pill is keyed on `verified_email`
  // presence (the wire's verified signal), with `verified` boolean
  // as the legacy fallback. See #371.
  const displayValue = id.verified_email ?? id.subject ?? id.value ?? "";
  const isVerified = Boolean(id.verified_email) || id.verified === true;

  return (
    <li className="flex items-center justify-between p-3 text-sm">
      <div className="min-w-0 flex-1">
        <p className="font-medium">{displayValue || "(unknown)"}</p>
        <p className="text-xs text-zinc-500">
          {identifierKindLabel(id.kind)}
          {isVerified ? (
            <span className="ml-2 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300">
              Verified
            </span>
          ) : (
            <span className="ml-2 rounded-full bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
              Pending
            </span>
          )}
        </p>
      </div>
      <Button
        size="sm"
        variant="outline"
        onClick={onRemove}
        disabled={pending}
        data-testid="remove-identifier"
      >
        {pending ? "Removing…" : "Remove"}
      </Button>
    </li>
  );
}
