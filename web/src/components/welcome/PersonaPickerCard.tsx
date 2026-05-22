"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { browserApi } from "@/lib/api";

/**
 * Client-side card for the /welcome persona picker (EPIC #422 piece 5).
 *
 * Flow per the founder direction in PR #445's welcome page:
 *   1. User clicks "Become a provider" / "Start building" / "Activate VPN".
 *   2. Card PUT /api/v1/me/preferred-landing-role with the picked role.
 *      The gateway-bff forwards to identity-svc which stamps it on the
 *      users.preferred_landing_role enum column (#449 chain).
 *   3. On success: router.push to the persona's virtual app + toast.
 *   4. On failure: toast.error + keep the user on /welcome so they can
 *      retry. We DON'T silently redirect — that would mask the persist
 *      failure and the next sign-in would re-show the picker.
 *
 * The auth-middleware redirect that uses preferred_landing_role to
 * decide /welcome vs /<role> at sign-in is a separate piece (gated on
 * a JWT claim refresh; tracked in the EPIC #422 follow-up). Today the
 * server-side persistence is what matters — that's what this card
 * writes.
 *
 * Empty-string role is intentionally not exposed here (the picker
 * never offers "clear my pick"); only the back-rail's Account section
 * can clear the value via a separate panel.
 */

export type ConsumerPersona = "provider" | "customer" | "vpn";

export interface PersonaPickerCardProps {
  /** Lucide icon component to render in the badge. */
  icon: React.ElementType;
  /** Display label on the pill chip ("Provider" / "Customer" / "VPN"). */
  badge: string;
  /** Heading copy. */
  title: string;
  /** Blurb body copy. */
  blurb: string;
  /** Primary CTA label. */
  cta: string;
  /** Persona id — drives both the API write + the post-success route. */
  persona: ConsumerPersona;
}

export function PersonaPickerCard({
  icon: Icon,
  badge,
  title,
  blurb,
  cta,
  persona,
}: PersonaPickerCardProps) {
  const router = useRouter();
  const [submitting, setSubmitting] = React.useState(false);

  const onClick = React.useCallback(async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await browserApi().put("/api/v1/me/preferred-landing-role", {
        role: persona,
      });
      // No toast on success — the redirect itself is the feedback.
      router.push(`/${persona}?from=welcome`);
    } catch (err) {
      toast.error(
        `Couldn't save your pick: ${(err as Error).message ?? "unknown error"}. Try again.`,
      );
      setSubmitting(false);
    }
  }, [persona, router, submitting]);

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={submitting}
      aria-busy={submitting}
      className="card group flex flex-col gap-4 text-left transition hover:border-primary-500 disabled:opacity-60"
      data-testid={`welcome-pick-${persona}`}
    >
      <div className="flex items-center gap-3">
        <span className="inline-flex h-10 w-10 items-center justify-center rounded-md bg-primary-50 text-primary-700 dark:bg-primary-900 dark:text-primary-200">
          <Icon className="h-5 w-5" aria-hidden />
        </span>
        <span className="pill">{badge}</span>
      </div>
      <h2 className="h-card text-foreground">{title}</h2>
      <p className="text-sm leading-relaxed text-muted-foreground">{blurb}</p>
      <span className="btn-primary mt-auto self-start">
        {submitting ? "Saving…" : `${cta} →`}
      </span>
    </button>
  );
}
