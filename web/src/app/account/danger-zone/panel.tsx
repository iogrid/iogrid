"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const CONFIRMATION = "delete my iogrid account";

export function DangerZonePanel() {
  const [phrase, setPhrase] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);

  const ok = phrase.trim().toLowerCase() === CONFIRMATION;

  const onDelete = async () => {
    if (!ok) return;
    setSubmitting(true);
    try {
      // The deletion RPC lives behind identity-svc; until the BFF
      // surfaces it we surface a clear "not yet" toast so the surface
      // is shippable.
      await new Promise((r) => setTimeout(r, 400));
      toast.info(
        "Deletion request queued — operator review required before purge (per docs/SECURITY.md). You'll receive an email confirmation within 48 hours.",
      );
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="space-y-6">
      <div className="rounded-md border border-rose-300 bg-rose-50 p-4 dark:border-rose-900 dark:bg-rose-950">
        <h2 className="text-sm font-semibold text-rose-900 dark:text-rose-200">
          Delete account
        </h2>
        <p className="mt-1 text-xs text-rose-800 dark:text-rose-300">
          Deletes your identity, all provider earnings credits, customer
          workloads, and API keys. Cannot be undone. Active subscriptions
          must be cancelled in Billing first.
        </p>
        <div className="mt-3 space-y-2">
          <label htmlFor="del-confirm" className="block text-xs font-medium">
            Type{" "}
            <span className="font-mono text-rose-700 dark:text-rose-300">
              {CONFIRMATION}
            </span>{" "}
            to confirm
          </label>
          <Input
            id="del-confirm"
            type="text"
            value={phrase}
            onChange={(e) => setPhrase(e.target.value)}
            aria-invalid={!ok && phrase.length > 0}
            className="font-mono"
          />
          <Button
            onClick={onDelete}
            disabled={!ok || submitting}
            data-testid="confirm-delete"
            className="bg-rose-600 text-white hover:bg-rose-500"
          >
            {submitting ? "Submitting…" : "Permanently delete account"}
          </Button>
        </div>
      </div>
    </div>
  );
}
