"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { StatsCard } from "@/components/dashboard/stats-card";
import { browserApi, ApiError } from "@/lib/api";
import type { CustomerBalance, VPNAccount } from "@/lib/types";

/**
 * /customer/billing — prepaid $GRID BALANCE surface (#632).
 *
 * Founder-ruled money model: prepaid + small capped grace overage. The
 * customer consumes only the $GRID they hold and must top up first; the
 * balance MAY dip slightly negative — up to a fixed grace cap — and that
 * arrears MUST be settled on the next top-up before further consumption.
 * Never unbounded negative.
 *
 * We lead with the current $GRID balance + a top-up CTA, then surface the
 * grace-overage cap + amount owed, then the bandwidth-consumption context
 * (tier/quota) as secondary info.
 *
 * Anti-fake-state guardrail (#417): we MUST NOT render a fake "FREE" /
 * "$0.00" balance when billing-svc / the Solana read is down — that's
 * visually identical to a real zero balance and hides the outage. On any
 * non-actionable failure we show an explicit error banner with a Retry.
 * The ONE actionable, non-error state is "no wallet bound" (409), which
 * renders a "connect wallet" CTA instead of a red banner.
 */

const GRID_DECIMALS = 1_000_000_000; // $GRID has 9 decimals.

/** Render an atomic (9-decimal) $GRID amount as a signed decimal string. */
function fmtGrid(atomic: number, dp = 4): string {
  const sign = atomic < 0 ? "-" : "";
  const abs = Math.abs(atomic) / GRID_DECIMALS;
  return `${sign}${abs.toLocaleString(undefined, {
    minimumFractionDigits: dp,
    maximumFractionDigits: dp,
  })}`;
}

export function BillingPanel() {
  const [wsId, setWsId] = React.useState<string | null>(null);
  const [balance, setBalance] = React.useState<CustomerBalance | null>(null);
  const [account, setAccount] = React.useState<VPNAccount | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [fetchError, setFetchError] = React.useState<string | null>(null);
  const [noWallet, setNoWallet] = React.useState(false);
  const [reloadTick, setReloadTick] = React.useState(0);

  React.useEffect(() => {
    setWsId(localStorage.getItem("iogrid_workspace_id"));
  }, []);

  React.useEffect(() => {
    if (!wsId) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setFetchError(null);
    setNoWallet(false);
    setBalance(null);
    setAccount(null);

    const api = browserApi();
    // Balance is the primary read; the VPN account (tier/quota) is
    // best-effort context shown alongside. A failed account read MUST
    // NOT block the balance card, and a failed balance read MUST surface
    // explicitly rather than rendering a fake $0.
    Promise.allSettled([
      api.get<CustomerBalance>("/api/v1/customer/billing/balance"),
      api.get<VPNAccount>(`/api/v1/vpn/account?workspace_id=${wsId}`),
    ])
      .then(([balRes, acctRes]) => {
        if (cancelled) return;

        if (balRes.status === "fulfilled") {
          const b = balRes.value;
          // Trust-boundary check: a 200 with a body missing the canonical
          // numeric fields is treated as an error, not a fake zero (#417).
          if (
            !b ||
            typeof b.balance_atomic !== "number" ||
            typeof b.available_atomic !== "number"
          ) {
            setFetchError("Balance service returned a malformed response.");
          } else {
            setBalance(b);
          }
        } else {
          const err = balRes.reason;
          if (err instanceof ApiError && err.code === "no_wallet_bound") {
            // Actionable, not an outage — render the connect-wallet CTA.
            setNoWallet(true);
          } else {
            const msg = (err as Error)?.message || "Unknown error";
            setFetchError(msg);
            toast.error(`Balance unavailable: ${msg}`);
          }
        }

        if (acctRes.status === "fulfilled") {
          const a = acctRes.value;
          if (a && typeof a.tier === "string") setAccount(a);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [wsId, reloadTick]);

  const onRetry = () => setReloadTick((n) => n + 1);

  const onTopUp = () => {
    // Top-up routes through the wallet surface where the user holds /
    // funds their Solana wallet with $GRID. Settings → Wallet is the
    // canonical bind + top-up entry point.
    window.location.href = "/account/wallets";
  };

  if (!wsId) {
    return (
      <div className="rounded-md border border-warning/30 bg-warning/10 p-4 text-sm text-warning dark:border-warning/40 dark:bg-warning/15 dark:text-warning">
        Bind a workspace on the Overview tab first.
      </div>
    );
  }
  if (loading) {
    return (
      <div className="rounded-md border border-border p-8 text-center text-sm text-muted-foreground dark:border-border">
        Loading balance…
      </div>
    );
  }
  if (noWallet) {
    return (
      <div
        data-testid="billing-no-wallet"
        className="flex flex-col gap-3 rounded-md border border-border bg-card p-6 dark:border-border"
      >
        <div>
          <h2 className="text-sm font-medium">Connect a wallet to fund $GRID</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            iogrid is prepaid: you consume only the $GRID you hold. Bind a
            Solana wallet, top it up with $GRID, and your balance appears
            here. Consumption draws down the balance per byte.
          </p>
        </div>
        <div>
          <Button onClick={onTopUp} data-testid="billing-connect-wallet">
            Connect wallet
          </Button>
        </div>
      </div>
    );
  }
  if (fetchError) {
    return (
      <div
        role="alert"
        data-testid="billing-fetch-error"
        className="flex flex-col gap-3 rounded-md border border-destructive/40 bg-destructive/10 p-4 text-sm text-destructive dark:border-destructive/40 dark:bg-destructive/15 dark:text-destructive sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <p className="font-medium">Balance temporarily unavailable</p>
          <p className="mt-1 text-xs opacity-80">
            We couldn&apos;t load your prepaid $GRID balance. Please retry in
            a moment. ({fetchError})
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={onRetry}
          data-testid="billing-retry"
        >
          Retry
        </Button>
      </div>
    );
  }
  if (!balance) {
    // Defensive — loading is false, no error, no wallet-gate, so balance
    // MUST be set; surface the bug rather than a misleading "$0".
    return (
      <div
        role="alert"
        className="rounded-md border border-destructive/40 bg-destructive/10 p-4 text-sm text-destructive dark:border-destructive/40 dark:bg-destructive/15 dark:text-destructive"
      >
        Balance state is empty — please reload the page. If the problem
        persists, contact support.
      </div>
    );
  }

  const owed = balance.grace_overage_owed_atomic;
  const cap = balance.grace_overage_cap_atomic;
  const available = balance.available_atomic;
  const negative = available < 0;
  // How much grace headroom remains before consumption is blocked.
  const graceRemaining = Math.max(0, cap - Math.max(0, owed));

  const usedGB =
    account != null
      ? (account.bandwidth_used_bytes / 1024 ** 3).toFixed(1)
      : null;
  const bandwidthLabel =
    account == null
      ? "—"
      : account.bandwidth_quota_bytes === 0
        ? `${usedGB} GB / unlimited`
        : `${usedGB} / ${(account.bandwidth_quota_bytes / 1024 ** 3).toFixed(0)} GB`;

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <StatsCard
          label="Available $GRID"
          value={`${fmtGrid(available)}`}
          hint={
            negative
              ? "In grace overage — top up to clear"
              : "Prepaid spendable balance"
          }
          className={negative ? "border-warning/50" : undefined}
        />
        <StatsCard
          label="On-chain $GRID"
          value={balance.balance_grid}
          hint="Held in your bound wallet"
        />
        <StatsCard label="Bandwidth this cycle" value={bandwidthLabel} />
      </div>

      {/* Grace-overage status — explicit so the prepaid contract is legible. */}
      <section
        data-testid="grace-overage"
        className={
          owed > 0
            ? "rounded-md border border-warning/40 bg-warning/10 p-4 dark:border-warning/40 dark:bg-warning/15"
            : "rounded-md border border-border bg-card p-4 dark:border-border"
        }
      >
        <h2 className="text-sm font-medium">Grace overage</h2>
        {owed > 0 ? (
          <p className="mt-1 text-xs text-muted-foreground">
            Your balance ran <span className="font-medium">{fmtGrid(owed)} $GRID</span>{" "}
            into grace (cap {fmtGrid(cap, 2)} $GRID). This arrears must be
            cleared on your next top-up before further consumption.
          </p>
        ) : (
          <p className="mt-1 text-xs text-muted-foreground">
            You have <span className="font-medium">{fmtGrid(graceRemaining, 2)} $GRID</span>{" "}
            of grace headroom (cap {fmtGrid(cap, 2)} $GRID). Consumption may
            run slightly past zero up to this cap; it&apos;s settled on your
            next top-up.
          </p>
        )}
      </section>

      <section className="rounded-md border border-border bg-card p-4 dark:border-border">
        <h2 className="text-sm font-medium">Top up $GRID</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          iogrid is prepaid — add $GRID to your bound wallet to keep
          consuming bandwidth and compute. Per-byte consumption draws this
          balance down{owed > 0 ? "; your next top-up first clears the grace overage above" : ""}.
        </p>
        <Button className="mt-3" onClick={onTopUp} data-testid="billing-topup">
          Top up balance
        </Button>
      </section>
    </div>
  );
}
