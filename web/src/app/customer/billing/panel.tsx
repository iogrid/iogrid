"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { StatsCard } from "@/components/dashboard/stats-card";
import { browserApi } from "@/lib/api";
import type { VPNAccount, CheckoutSessionResponse } from "@/lib/types";

/**
 * /customer/billing — we treat the existing /api/v1/vpn/account
 * endpoint as the source of truth for the customer's subscription
 * (same billing-svc record, single Stripe customer per workspace).
 * The "Manage in Stripe" button opens the Customer Portal.
 */
export function BillingPanel() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [account, setAccount] = React.useState<VPNAccount | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [opening, setOpening] = React.useState(false);

  React.useEffect(() => {
    setWsId(localStorage.getItem("iogrid_workspace_id"));
  }, []);

  React.useEffect(() => {
    if (!wsId) {
      setLoading(false);
      return;
    }
    browserApi()
      .get<VPNAccount>(`/api/v1/vpn/account?workspace_id=${wsId}`)
      .then(setAccount)
      .catch((e) => toast.error((e as Error).message))
      .finally(() => setLoading(false));
  }, [wsId]);

  const onManage = async () => {
    if (!wsId) return;
    setOpening(true);
    try {
      const res = await browserApi().post<CheckoutSessionResponse>(
        "/api/v1/vpn/upgrade",
        {
          workspace_id: wsId,
          tier: "growth",
          success_url: `${window.location.origin}/customer/billing?status=success`,
          cancel_url: `${window.location.origin}/customer/billing?status=cancelled`,
        },
      );
      window.location.href = res.checkoutUrl;
    } catch (e) {
      toast.error(`Open portal failed: ${(e as Error).message}`);
    } finally {
      setOpening(false);
    }
  };

  if (!wsId) {
    return (
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">
        Bind a workspace on the Overview tab first.
      </div>
    );
  }
  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading subscription…
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <StatsCard label="Tier" value={account?.tier ?? "FREE"} />
        <StatsCard label="Status" value={account?.status ?? "—"} />
        <StatsCard
          label="Bandwidth quota"
          value={
            account
              ? `${(account.bandwidth_used_bytes / 1024 ** 3).toFixed(1)} / ${(
                  account.bandwidth_quota_bytes /
                  1024 ** 3
                ).toFixed(0)} GB`
              : "—"
          }
        />
      </div>

      <section className="rounded-md border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
        <h2 className="text-sm font-medium">Subscription</h2>
        <p className="mt-1 text-xs text-zinc-500">
          Payment method, invoices, plan changes — all live in the Stripe
          Customer Portal. The button below opens a Checkout session for an
          upgrade or, if you already have a subscription, the portal lands
          on the manage-payment screen.
        </p>
        <Button
          className="mt-3"
          onClick={onManage}
          disabled={opening || !account?.upgrade_available}
          data-testid="open-stripe-portal"
        >
          {opening ? "Opening Stripe…" : "Manage in Stripe"}
        </Button>
        {!account?.upgrade_available ? (
          <p className="mt-2 text-xs text-zinc-500">
            You&apos;re on the highest tier; no upgrades available.
          </p>
        ) : null}
      </section>
    </div>
  );
}
