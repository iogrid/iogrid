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

const PLANS: Plan[] = [
  {
    tier: "starter",
    label: "Plus",
    price: "$4 / mo",
    bullets: ["200 GB / month", "5 simultaneous regions", "Standard support"],
  },
  {
    tier: "growth",
    label: "Pro",
    price: "$12 / mo",
    bullets: [
      "2 TB / month",
      "All regions",
      "Per-app exit selection",
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
