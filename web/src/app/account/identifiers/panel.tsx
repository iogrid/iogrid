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
      <div className="rounded-md border border-border p-8 text-center text-sm text-muted-foreground dark:border-border">
        Loading identifiers…
      </div>
    );
  }
  if (!me) {
    return (
      <div className="rounded-md border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive dark:border-destructive/40 dark:bg-destructive/15 dark:text-destructive">
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
      <ul className="divide-y divide-border rounded-md border border-border dark:divide-border dark:border-border">
        {(me.identifiers ?? []).map((id) => (
          <IdentifierRow
            key={id.id?.value ?? id.value}
            id={id}
            onRemoved={onRemoved}
            onError={refresh}
          />
        ))}
        {(me.identifiers ?? []).length === 0 ? (
          <li className="p-4 text-sm text-muted-foreground">
            No identifiers bound. Add one to keep the account recoverable.
          </li>
        ) : null}
      </ul>
      <p className="text-xs text-muted-foreground">
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

  // Wire shape: gateway-bff GetMe now emits canonical proto3-JSON via
  // protojson (#801) → `verifiedEmail` (camelCase). The snake_case
  // `verified_email` twin is the pre-#801 stdlib fallback (still honoured
  // so a stale BFF pod mid-roll renders). `subject` covers OAuth
  // identifiers; `value` is the pre-#371 legacy fallback. The Verified
  // pill keys on a verified-email being present (canonical or fallback),
  // with the `verified` boolean as the oldest legacy signal. See
  // #801 + #371.
  const verifiedEmail = id.verifiedEmail ?? id.verified_email;
  const displayValue = verifiedEmail ?? id.subject ?? id.value ?? "";
  const isVerified = Boolean(verifiedEmail) || id.verified === true;

  return (
    <li className="flex items-center justify-between p-3 text-sm">
      <div className="min-w-0 flex-1">
        <p className="font-medium">{displayValue || "(unknown)"}</p>
        <p className="text-xs text-muted-foreground">
          {identifierKindLabel(id.kind)}
          {isVerified ? (
            <span className="ml-2 rounded-full bg-success/15 px-1.5 py-0.5 text-[10px] font-medium text-success dark:bg-success/15 dark:text-success">
              Verified
            </span>
          ) : (
            <span className="ml-2 rounded-full bg-warning/15 px-1.5 py-0.5 text-[10px] font-medium text-warning dark:bg-warning/15 dark:text-warning">
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
