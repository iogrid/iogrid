"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi } from "@/lib/api";
import { cn } from "@/lib/utils";
import type { CheckoutSessionResponse } from "@/lib/types";

interface Plan {
  tier: "starter" | "growth" | "enterprise";
  label: string;
  price: string;
  bullets: string[];
  highlight?: boolean;
}

// Pricing is the single source of truth from docs/BUSINESS-STRATEGY.md
// §1 line 52 + §3.3 table line 277 + §6.5 competitor matrix line 149:
// $0 free / $2.99 Plus / $4.99 Pro. The /vpn marketing page (PR #441)
// renders the same numbers; this panel MUST match — divergence is a
// trust killer for first-paying customers (Refs #441).
//
// Tier wire enums (starter/growth) map to billing-svc's
// SubscriptionTier proto, which is shared with the B2B compute tiers.
// The actual amount Stripe charges is determined by STRIPE_PRICE_STARTER
// / STRIPE_PRICE_GROWTH env Price IDs in billing-svc-secrets — confirm
// those reference Stripe Products with the canonical $2.99 / $4.99
// unit_amount before the public Stripe live flip (separate ticket).
const PLANS: Plan[] = [
  {
    tier: "starter",
    label: "Plus",
    price: "$2.99 / mo",
    bullets: [
      "Unlimited bandwidth",
      "All exit regions",
      "Up to 5 devices",
      "Standard support",
    ],
  },
  {
    tier: "growth",
    label: "Pro",
    price: "$4.99 / mo",
    bullets: [
      "Unlimited bandwidth",
      "Per-app exit selection",
      "Up to 10 devices",
      "DNS-over-HTTPS + tracker blocking",
      "Priority support",
    ],
    highlight: true,
  },
  {
    tier: "enterprise",
    label: "Enterprise",
    price: "Talk to us",
    bullets: [
      "Dedicated subnets",
      "SSO + SCIM",
      "SLA on bandwidth + latency",
      "Quarterly business reviews",
    ],
  },
];

export function UpgradePanel() {
  const [busyTier, setBusyTier] = React.useState<string | null>(null);

  const onUpgrade = async (tier: Plan["tier"]) => {
    setBusyTier(tier);
    try {
      const wsId = localStorage.getItem("iogrid_workspace_id");
      if (!wsId) {
        toast.error(
          "Bind a workspace on /customer first — every Stripe subscription is scoped to a workspace.",
        );
        return;
      }
      const res = await browserApi().post<CheckoutSessionResponse>(
        "/api/v1/vpn/upgrade",
        {
          workspace_id: wsId,
          tier,
          success_url: `${window.location.origin}/customer/billing?status=success`,
          cancel_url: `${window.location.origin}/vpn/upgrade`,
        },
      );
      window.location.href = res.checkoutUrl;
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusyTier(null);
    }
  };

  return (
    <section className="mt-10 grid grid-cols-1 gap-4 md:grid-cols-3">
      {PLANS.map((p) => (
        <article
          key={p.tier}
          className={cn(
            "rounded-lg border p-5",
            p.highlight
              ? "border-success/40 bg-success/10 dark:bg-success/15"
              : "border-border bg-card dark:border-border",
          )}
        >
          <h2 className="text-xl font-bold">{p.label}</h2>
          <p className="mt-1 text-2xl font-semibold">{p.price}</p>
          <ul className="mt-3 space-y-1 text-sm">
            {p.bullets.map((b) => (
              <li key={b} className="flex items-start gap-2">
                <span aria-hidden className="mt-0.5 text-success">✓</span>
                <span>{b}</span>
              </li>
            ))}
          </ul>
          <Button
            className="mt-4 w-full"
            onClick={() => onUpgrade(p.tier)}
            disabled={busyTier !== null}
          >
            {busyTier === p.tier ? "Opening Stripe…" : `Choose ${p.label}`}
          </Button>
        </article>
      ))}
    </section>
  );
}
