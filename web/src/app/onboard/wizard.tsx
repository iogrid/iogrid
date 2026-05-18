"use client";

import { useState, useEffect, useTransition } from "react";
import { useRouter } from "next/navigation";

/**
 * Sensible-defaults wizard — three steps, all skippable to defaults.
 *
 * Step 1: Resource caps (bandwidth + CPU).
 * Step 2: Categories the provider accepts.
 * Step 3: Payout method.
 *
 * On mount, calls /api/v1/onboard/start to link the pairing token to
 * the authenticated user. On submit, calls /api/v1/onboard/complete
 * with the chosen defaults, then routes to /provide.
 *
 * All form state is purely client-side React; we don't try to round-trip
 * partial state to the server until the user hits "Finish".
 */

type WizardStep = 1 | 2 | 3 | 4; // 4 = welcome / confetti

const DEFAULT_CATEGORIES = [
  { id: "ecommerce", label: "E-commerce price monitoring", default: true },
  { id: "seo", label: "SEO rank tracking", default: true },
  { id: "ad_verification", label: "Ad verification", default: true },
  { id: "ai_training", label: "AI training data collection", default: true },
  { id: "iogrid_internal", label: "iogrid internal", default: true },
  { id: "lead_gen", label: "Lead generation (LinkedIn, Indeed, …)", default: false },
  { id: "social_intel", label: "Social media intelligence", default: false },
  { id: "adult", label: "Adult content scraping (requires extra confirmation)", default: false },
];

type Defaults = {
  bandwidth_cap_gb: number;
  cpu_cap_pct: number;
  idle_only: boolean;
  calendar: string;
  categories: string[];
  payout_tier: "defer" | "stripe_connect" | "iogrid_credit";
};

const INITIAL: Defaults = {
  bandwidth_cap_gb: 50,
  cpu_cap_pct: 30,
  idle_only: true,
  calendar: "",
  categories: DEFAULT_CATEGORIES.filter((c) => c.default).map((c) => c.id),
  payout_tier: "defer",
};

export function OnboardingWizard({ token }: { token: string }) {
  const router = useRouter();
  const [step, setStep] = useState<WizardStep>(1);
  const [defaults, setDefaults] = useState<Defaults>(INITIAL);
  const [submitting, startSubmitting] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const [linked, setLinked] = useState(false);

  // Step 0: link the token to this user on mount. Idempotent — re-runs
  // are safe per BFF contract (Link is upsert-friendly when uid matches).
  useEffect(() => {
    let aborted = false;
    (async () => {
      try {
        const res = await fetch("/api/onboard/start", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ token }),
        });
        if (!aborted) {
          if (!res.ok) {
            const body = (await res.json().catch(() => ({}))) as {
              message?: string;
            };
            setError(
              body.message ??
                `Could not link this device (status ${res.status}).`,
            );
          } else {
            setLinked(true);
          }
        }
      } catch (err) {
        if (!aborted) {
          setError(
            err instanceof Error
              ? `Network error: ${err.message}`
              : "Network error while linking device",
          );
        }
      }
    })();
    return () => {
      aborted = true;
    };
  }, [token]);

  function onSubmit() {
    setError(null);
    startSubmitting(async () => {
      try {
        const res = await fetch("/api/onboard/complete", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ token, defaults }),
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as {
            message?: string;
          };
          setError(body.message ?? `Submission failed (status ${res.status}).`);
          return;
        }
        setStep(4);
        // Brief celebration, then hand off to the provider dashboard.
        setTimeout(() => {
          router.push("/provide");
        }, 2500);
      } catch (err) {
        setError(
          err instanceof Error
            ? `Network error: ${err.message}`
            : "Network error during submission",
        );
      }
    });
  }

  function toggleCategory(id: string) {
    setDefaults((d) => ({
      ...d,
      categories: d.categories.includes(id)
        ? d.categories.filter((c) => c !== id)
        : [...d.categories, id],
    }));
  }

  return (
    <section aria-label="Onboarding wizard" className="space-y-6">
      <ProgressBar step={step} />

      {error && (
        <div
          role="alert"
          className="rounded-md border border-red-300 bg-red-50 p-4 text-sm text-red-900"
        >
          {error}
        </div>
      )}

      {step === 1 && (
        <StepCaps
          defaults={defaults}
          setDefaults={setDefaults}
          onNext={() => setStep(2)}
        />
      )}

      {step === 2 && (
        <StepCategories
          defaults={defaults}
          toggleCategory={toggleCategory}
          onBack={() => setStep(1)}
          onNext={() => setStep(3)}
        />
      )}

      {step === 3 && (
        <StepPayout
          defaults={defaults}
          setDefaults={setDefaults}
          onBack={() => setStep(2)}
          onFinish={onSubmit}
          submitting={submitting}
          linked={linked}
        />
      )}

      {step === 4 && <Welcome />}
    </section>
  );
}

function ProgressBar({ step }: { step: WizardStep }) {
  const labels = ["Resources", "Categories", "Payout"];
  return (
    <ol className="grid grid-cols-3 gap-2 text-xs">
      {labels.map((label, i) => {
        const n = (i + 1) as 1 | 2 | 3;
        const active = step === n || (step === 4 && n === 3);
        const done = step > n;
        return (
          <li
            key={label}
            className={`flex items-center gap-2 rounded-md border px-3 py-2 ${
              active
                ? "border-zinc-900 bg-zinc-900 text-white"
                : done
                  ? "border-zinc-300 bg-zinc-100 text-zinc-700"
                  : "border-zinc-200 text-zinc-500"
            }`}
          >
            <span className="font-mono">{n}</span>
            <span className="font-medium">{label}</span>
          </li>
        );
      })}
    </ol>
  );
}

function StepCaps({
  defaults,
  setDefaults,
  onNext,
}: {
  defaults: Defaults;
  setDefaults: (fn: (d: Defaults) => Defaults) => void;
  onNext: () => void;
}) {
  return (
    <fieldset className="space-y-4 rounded-lg border border-zinc-200 p-6">
      <legend className="px-2 text-sm font-medium text-zinc-700">
        Step 1 of 3 — Resource caps
      </legend>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        We use sensible defaults that won&apos;t slow down your machine. Adjust
        if you want.
      </p>

      <label className="block space-y-1">
        <span className="text-sm font-medium">Bandwidth per month (GB)</span>
        <input
          type="range"
          min={5}
          max={500}
          step={5}
          value={defaults.bandwidth_cap_gb}
          onChange={(e) =>
            setDefaults((d) => ({
              ...d,
              bandwidth_cap_gb: Number(e.target.value),
            }))
          }
          className="w-full"
          aria-label="Bandwidth cap in gigabytes per month"
        />
        <div className="text-xs text-zinc-500">
          Current: <strong>{defaults.bandwidth_cap_gb} GB / month</strong>{" "}
          (typical home plan is 1–2 TB)
        </div>
      </label>

      <label className="block space-y-1">
        <span className="text-sm font-medium">CPU usage cap (%)</span>
        <input
          type="range"
          min={5}
          max={90}
          step={5}
          value={defaults.cpu_cap_pct}
          onChange={(e) =>
            setDefaults((d) => ({
              ...d,
              cpu_cap_pct: Number(e.target.value),
            }))
          }
          className="w-full"
          aria-label="CPU cap in percent"
        />
        <div className="text-xs text-zinc-500">
          Current: <strong>{defaults.cpu_cap_pct}%</strong> (lower = your apps
          stay snappier)
        </div>
      </label>

      <label className="flex items-start gap-3">
        <input
          type="checkbox"
          checked={defaults.idle_only}
          onChange={(e) =>
            setDefaults((d) => ({ ...d, idle_only: e.target.checked }))
          }
          className="mt-1 h-4 w-4"
        />
        <span className="text-sm">
          <strong>Only run when I&apos;m away.</strong> Detects idle for 5
          minutes before accepting work. Zero impact when you&apos;re using your
          machine.
        </span>
      </label>

      <div className="flex justify-end">
        <button
          type="button"
          onClick={onNext}
          className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700"
        >
          Next
        </button>
      </div>
    </fieldset>
  );
}

function StepCategories({
  defaults,
  toggleCategory,
  onBack,
  onNext,
}: {
  defaults: Defaults;
  toggleCategory: (id: string) => void;
  onBack: () => void;
  onNext: () => void;
}) {
  return (
    <fieldset className="space-y-4 rounded-lg border border-zinc-200 p-6">
      <legend className="px-2 text-sm font-medium text-zinc-700">
        Step 2 of 3 — What kinds of traffic do you accept?
      </legend>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        Anti-abuse + CSAM filtering applies to all categories. You can change
        these any time.
      </p>
      <div className="space-y-2">
        {DEFAULT_CATEGORIES.map((c) => (
          <label
            key={c.id}
            className="flex items-start gap-3 rounded-md border border-zinc-200 px-3 py-2 hover:bg-zinc-50"
          >
            <input
              type="checkbox"
              checked={defaults.categories.includes(c.id)}
              onChange={() => toggleCategory(c.id)}
              className="mt-1 h-4 w-4"
            />
            <span className="text-sm">{c.label}</span>
          </label>
        ))}
      </div>
      <div className="flex justify-between">
        <button
          type="button"
          onClick={onBack}
          className="rounded-md border border-zinc-300 px-4 py-2 text-sm font-medium hover:bg-zinc-50"
        >
          Back
        </button>
        <button
          type="button"
          onClick={onNext}
          className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700"
        >
          Next
        </button>
      </div>
    </fieldset>
  );
}

function StepPayout({
  defaults,
  setDefaults,
  onBack,
  onFinish,
  submitting,
  linked,
}: {
  defaults: Defaults;
  setDefaults: (fn: (d: Defaults) => Defaults) => void;
  onBack: () => void;
  onFinish: () => void;
  submitting: boolean;
  linked: boolean;
}) {
  return (
    <fieldset className="space-y-4 rounded-lg border border-zinc-200 p-6">
      <legend className="px-2 text-sm font-medium text-zinc-700">
        Step 3 of 3 — How would you like to be paid?
      </legend>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        Pick now or pick later from your dashboard.
      </p>
      <div className="space-y-2">
        {(
          [
            {
              id: "defer",
              label: "Decide later",
              hint: "Earnings accumulate on your account; pick a payout method any time.",
            },
            {
              id: "stripe_connect",
              label: "Direct deposit (Stripe Connect)",
              hint: "Bank account or debit card; ~3 day delay; minimum $25.",
            },
            {
              id: "iogrid_credit",
              label: "iogrid credit",
              hint: "Use earnings to pay for iogrid services (proxies, builds). 5% bonus.",
            },
          ] as const
        ).map((opt) => (
          <label
            key={opt.id}
            className={`flex cursor-pointer items-start gap-3 rounded-md border px-3 py-3 ${
              defaults.payout_tier === opt.id
                ? "border-zinc-900 bg-zinc-50"
                : "border-zinc-200 hover:bg-zinc-50"
            }`}
          >
            <input
              type="radio"
              name="payout_tier"
              value={opt.id}
              checked={defaults.payout_tier === opt.id}
              onChange={() =>
                setDefaults((d) => ({ ...d, payout_tier: opt.id }))
              }
              className="mt-1 h-4 w-4"
            />
            <span>
              <span className="block text-sm font-medium">{opt.label}</span>
              <span className="block text-xs text-zinc-500">{opt.hint}</span>
            </span>
          </label>
        ))}
      </div>

      {!linked && (
        <p className="text-xs text-yellow-700">
          Linking this device to your account… you can still submit;
          we&apos;ll finish linking in the background.
        </p>
      )}

      <div className="flex justify-between">
        <button
          type="button"
          onClick={onBack}
          className="rounded-md border border-zinc-300 px-4 py-2 text-sm font-medium hover:bg-zinc-50"
          disabled={submitting}
        >
          Back
        </button>
        <button
          type="button"
          onClick={onFinish}
          className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white hover:bg-zinc-700 disabled:opacity-50"
          disabled={submitting}
        >
          {submitting ? "Saving…" : "Finish setup"}
        </button>
      </div>
    </fieldset>
  );
}

function Welcome() {
  return (
    <div className="rounded-lg border border-zinc-200 bg-zinc-50 p-8 text-center">
      <div
        aria-hidden="true"
        className="mx-auto mb-4 text-5xl"
        style={{ animation: "iogrid-bounce 0.6s ease-out infinite alternate" }}
      >
        🎉
      </div>
      <h2 className="text-2xl font-bold">You&apos;re set up.</h2>
      <p className="mt-2 text-sm text-zinc-600">
        Your machine will start picking up workloads when it&apos;s idle.
        Taking you to your dashboard…
      </p>
      <style>{`
        @keyframes iogrid-bounce {
          from { transform: translateY(0) scale(1); }
          to   { transform: translateY(-8px) scale(1.08); }
        }
      `}</style>
    </div>
  );
}
