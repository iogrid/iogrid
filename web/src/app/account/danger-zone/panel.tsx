"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError, browserApi } from "@/lib/api";

const CONFIRMATION = "delete my iogrid account";

interface DeleteAccountResponse {
  deletedAt?: string;
  deleted_at?: string;
  sessionsRevoked?: number;
  sessions_revoked?: number;
}

export function DangerZonePanel() {
  const [phrase, setPhrase] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);
  const [reason, setReason] = React.useState("");

  const ok = phrase.trim().toLowerCase() === CONFIRMATION;

  const onDelete = async () => {
    if (!ok) return;
    setSubmitting(true);
    try {
      const resp = await browserApi().delWithBody<DeleteAccountResponse>(
        "/api/v1/me",
        { reason },
      );
      const revoked = resp?.sessionsRevoked ?? resp?.sessions_revoked ?? 0;
      toast.success(
        `Account scheduled for deletion. ${revoked} active session${
          revoked === 1 ? "" : "s"
        } revoked. You'll be signed out shortly.`,
      );
      // Identity-svc revoked every session — the next request will 401.
      // Bounce to the marketing root so the user lands somewhere sane
      // instead of an auth-protected page mid-redirect.
      window.setTimeout(() => {
        window.location.href = "/";
      }, 1500);
    } catch (e) {
      if (e instanceof ApiError && e.code === "step_up_required") {
        toast.error(
          "Re-authentication required. We'll email you a step-up link before deleting your account.",
        );
        // Kick off the step-up magic-link in the background so the user
        // can complete it without a manual nav step.
        try {
          await browserApi().post("/api/v1/account/step-up/request", {
            reason: "STEP_UP_REASON_ACCOUNT_DELETE",
          });
          toast.info("Check your email for the step-up link, then retry.");
        } catch {
          // Step-up endpoint not wired yet — the toast above is enough.
        }
      } else {
        toast.error((e as Error).message);
      }
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
          <label htmlFor="del-reason" className="block text-xs font-medium">
            Optional: tell us why you&apos;re leaving (audit log only)
          </label>
          <Input
            id="del-reason"
            type="text"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="e.g. switching providers, no longer needed"
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
