"use client";

import * as React from "react";
import { z } from "zod";
import { toast } from "sonner";
import {
  ScheduleCalendar,
  bitmasksToCalendarWindows,
  calendarWindowsToBitmasks,
} from "@/components/dashboard/schedule-calendar";
import { CategoryGrid } from "@/components/dashboard/category-grid";
import {
  ProviderEmptyState,
  PROVIDER_EMPTY_SCHEDULE_SUBTITLE,
} from "@/components/dashboard/provider-empty-state";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { browserApi, ApiError } from "@/lib/api";
import { CATEGORIES } from "@/lib/categories";
import { formatBytes } from "@/lib/format";
import type {
  GetCurrentStateResponse,
  GetSchedulingConfigResponse,
  SchedulingConfig,
} from "@/lib/types";

/** Caps validation. UI-side only; the BFF re-validates on POST. */
const capsSchema = z.object({
  bandwidthCapGbPerMonth: z.number().int().nonnegative().max(100_000),
  cpuCapPercent: z.number().int().min(0).max(100),
  memoryCapPercent: z.number().int().min(0).max(100),
  gpuCapPercentWhenIdle: z.number().int().min(0).max(100),
  gpuCapPercentWhenActive: z.number().int().min(0).max(100),
});

interface FormState {
  bandwidthCap: number;
  cpuCap: number;
  memoryCap: number;
  gpuIdle: number;
  gpuActive: number;
  idleEnabled: boolean;
  idleThreshold: number;
  windows: number[]; // 7×24 bitmask
  categories: string[];
  blocklist: string;
  perCustomerMinutes: number;
  perCustomerEnabled: boolean;
  tester: string;
  timezone: string;
}

const DEFAULT_FORM: FormState = {
  bandwidthCap: 50,
  cpuCap: 50,
  memoryCap: 30,
  gpuIdle: 80,
  gpuActive: 0,
  idleEnabled: true,
  idleThreshold: 300,
  windows: new Array<number>(7).fill(0xffffff),
  categories: CATEGORIES.map((c) => c.slug),
  blocklist: "",
  perCustomerMinutes: 30,
  perCustomerEnabled: true,
  tester: "",
  timezone: typeof Intl !== "undefined"
    ? Intl.DateTimeFormat().resolvedOptions().timeZone
    : "UTC",
};

export function ScheduleEditor() {
  const [form, setForm] = React.useState<FormState>(DEFAULT_FORM);
  const [usage, setUsage] = React.useState<GetCurrentStateResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  // hasProvider mirrors gateway-bff's schedule envelope flag (#313). We
  // gate the form render on this so users with zero paired daemons see
  // the "Install daemon" CTA instead of a form they cannot meaningfully
  // submit (the BFF would 403 on POST without an owned provider).
  const [hasProvider, setHasProvider] = React.useState<boolean | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const api = browserApi();
        const [cfgRes, stateRes] = await Promise.all([
          api
            .get<GetSchedulingConfigResponse>("/api/v1/provide/schedule")
            .catch((e: ApiError) => {
              if (e.status === 404) return { config: undefined };
              throw e;
            }),
          api
            .get<GetCurrentStateResponse>("/api/v1/provide/dashboard")
            .catch(() => null),
        ]);
        if (cancelled) return;
        if (stateRes) setUsage(stateRes);
        // Treat anything other than an explicit `false` as "has provider"
        // — covers older BFF builds that didn't emit the flag yet.
        setHasProvider(cfgRes.has_provider === false ? false : true);
        const cfg = cfgRes.config;
        if (cfg) {
          setForm({
            ...DEFAULT_FORM,
            bandwidthCap: cfg.caps?.bandwidthCapGbPerMonth ?? DEFAULT_FORM.bandwidthCap,
            cpuCap: cfg.caps?.cpuCapPercent ?? DEFAULT_FORM.cpuCap,
            memoryCap: cfg.caps?.memoryCapPercent ?? DEFAULT_FORM.memoryCap,
            gpuIdle: cfg.caps?.gpuCapPercentWhenIdle ?? DEFAULT_FORM.gpuIdle,
            gpuActive: cfg.caps?.gpuCapPercentWhenActive ?? DEFAULT_FORM.gpuActive,
            idleEnabled: cfg.idle?.enabled ?? DEFAULT_FORM.idleEnabled,
            idleThreshold:
              cfg.idle?.idleThresholdSeconds ?? DEFAULT_FORM.idleThreshold,
            windows: calendarWindowsToBitmasks(cfg.calendar?.windows ?? []),
            categories:
              cfg.categoryOptIn?.allowedCategories ?? DEFAULT_FORM.categories,
            blocklist: (
              cfg.destinationPolicy?.destinationBlocklist ?? []
            ).join("\n"),
            perCustomerMinutes:
              cfg.destinationPolicy?.perCustomerMinutesCap ??
              DEFAULT_FORM.perCustomerMinutes,
            perCustomerEnabled:
              (cfg.destinationPolicy?.perCustomerMinutesCap ?? 0) > 0,
            tester: "",
            timezone: DEFAULT_FORM.timezone,
          });
        }
      } catch (err) {
        toast.error(`Couldn't load schedule: ${(err as Error).message}`);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const onSave = async () => {
    const capsParsed = capsSchema.safeParse({
      bandwidthCapGbPerMonth: form.bandwidthCap,
      cpuCapPercent: form.cpuCap,
      memoryCapPercent: form.memoryCap,
      gpuCapPercentWhenIdle: form.gpuIdle,
      gpuCapPercentWhenActive: form.gpuActive,
    });
    if (!capsParsed.success) {
      const errs: Record<string, string> = {};
      for (const i of capsParsed.error.issues) {
        errs[i.path.join(".")] = i.message;
      }
      setErrors(errs);
      toast.error("Some caps look wrong; please review.");
      return;
    }
    setErrors({});
    setSaving(true);
    try {
      const cfg: SchedulingConfig = {
        caps: capsParsed.data,
        calendar: {
          windows: bitmasksToCalendarWindows(form.windows, form.timezone),
        },
        idle: {
          enabled: form.idleEnabled,
          idleThresholdSeconds: form.idleThreshold,
        },
        categoryOptIn: {
          allowedCategories: form.categories,
          disallowedCategories: CATEGORIES.map((c) => c.slug).filter(
            (s) => !form.categories.includes(s),
          ),
        },
        destinationPolicy: {
          destinationBlocklist: form.blocklist
            .split(/\r?\n/)
            .map((s) => s.trim())
            .filter(Boolean),
          perCustomerMinutesCap: form.perCustomerEnabled
            ? form.perCustomerMinutes
            : 0,
        },
      };
      await browserApi().post("/api/v1/provide/schedule", { config: cfg });
      toast.success("Schedule saved. Daemon will sync within seconds.");
    } catch (err) {
      toast.error(`Save failed: ${(err as Error).message}`);
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="rounded-md border border-zinc-200 p-8 text-center text-sm text-zinc-500 dark:border-zinc-800">
        Loading current schedule…
      </div>
    );
  }

  // Gate on ownership BEFORE rendering the editor form (#313). The
  // empty-state replaces the entire surface, NOT a partial overlay,
  // because every section (caps / calendar / categories) would require
  // an owned provider to persist.
  if (hasProvider === false) {
    return <ProviderEmptyState subtitle={PROVIDER_EMPTY_SCHEDULE_SUBTITLE} />;
  }

  return (
    <form
      data-testid="schedule-form"
      onSubmit={(e) => {
        e.preventDefault();
        void onSave();
      }}
      className="space-y-8"
    >
      <section aria-labelledby="caps-heading" className="space-y-3">
        <h2 id="caps-heading" className="text-lg font-semibold">
          Resource caps
        </h2>
        <CapSlider
          label="Bandwidth per month"
          value={form.bandwidthCap}
          onChange={(v) => setForm({ ...form, bandwidthCap: v })}
          unit=" GB"
          max={2000}
          used={usage?.usage ? Number(usage.usage.bandwidthUsedBytesThisMonth) : undefined}
          usedFormat={(b) => formatBytes(b)}
          capFormat={(v) => `${v} GB`}
          error={errors["bandwidthCapGbPerMonth"]}
        />
        <CapSlider
          label="CPU cap"
          value={form.cpuCap}
          onChange={(v) => setForm({ ...form, cpuCap: v })}
          unit="%"
          max={100}
          used={usage?.usage?.cpuPercent}
          usedFormat={(v) => `${v}%`}
          capFormat={(v) => `${v}%`}
          error={errors["cpuCapPercent"]}
        />
        <CapSlider
          label="Memory cap"
          value={form.memoryCap}
          onChange={(v) => setForm({ ...form, memoryCap: v })}
          unit="%"
          max={100}
          used={usage?.usage?.memoryPercent}
          usedFormat={(v) => `${v}%`}
          capFormat={(v) => `${v}%`}
          error={errors["memoryCapPercent"]}
        />
        <CapSlider
          label="GPU when idle"
          value={form.gpuIdle}
          onChange={(v) => setForm({ ...form, gpuIdle: v })}
          unit="%"
          max={100}
          used={usage?.usage?.gpuPercent}
          usedFormat={(v) => `${v}%`}
          capFormat={(v) => `${v}%`}
          error={errors["gpuCapPercentWhenIdle"]}
        />
        <CapSlider
          label="GPU when user is active"
          value={form.gpuActive}
          onChange={(v) => setForm({ ...form, gpuActive: v })}
          unit="%"
          max={100}
          capFormat={(v) => `${v}%`}
          error={errors["gpuCapPercentWhenActive"]}
        />
      </section>

      <section aria-labelledby="idle-heading" className="space-y-3">
        <h2 id="idle-heading" className="text-lg font-semibold">
          Idle detection
        </h2>
        <label className="flex items-center gap-3 text-sm">
          <input
            type="checkbox"
            checked={form.idleEnabled}
            onChange={(e) =>
              setForm({ ...form, idleEnabled: e.target.checked })
            }
            className="h-4 w-4 accent-emerald-600"
          />
          Only run when I have been idle for at least
          <Input
            type="number"
            min={0}
            max={3600}
            value={form.idleThreshold}
            onChange={(e) =>
              setForm({ ...form, idleThreshold: Number(e.target.value) })
            }
            className="h-8 w-20 text-sm"
            disabled={!form.idleEnabled}
            aria-label="Idle threshold seconds"
          />
          seconds
        </label>
      </section>

      <section aria-labelledby="calendar-heading" className="space-y-3">
        <h2 id="calendar-heading" className="text-lg font-semibold">
          Calendar
        </h2>
        <ScheduleCalendar
          value={form.windows}
          onChange={(w) => setForm({ ...form, windows: w })}
        />
      </section>

      <section aria-labelledby="cat-heading" className="space-y-3">
        <h2 id="cat-heading" className="text-lg font-semibold">
          Accepted categories
        </h2>
        <CategoryGrid
          categories={CATEGORIES}
          selected={form.categories}
          onToggle={(slug, on) =>
            setForm({
              ...form,
              categories: on
                ? Array.from(new Set([...form.categories, slug]))
                : form.categories.filter((c) => c !== slug),
            })
          }
        />
      </section>

      <section aria-labelledby="dest-heading" className="space-y-3">
        <h2 id="dest-heading" className="text-lg font-semibold">
          Destination blocklist
        </h2>
        <p className="text-sm text-zinc-600 dark:text-zinc-400">
          One pattern per line. Globs (<code>*.example.com</code>) and regex
          (<code>/^api\\./</code>) are both accepted. Customers cannot relay
          to anything matching a blocklist entry.
        </p>
        <textarea
          value={form.blocklist}
          onChange={(e) => setForm({ ...form, blocklist: e.target.value })}
          rows={6}
          spellCheck={false}
          className="block w-full rounded-md border border-zinc-300 bg-transparent px-3 py-2 font-mono text-xs placeholder:text-zinc-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-zinc-400 dark:border-zinc-700"
          placeholder="*.chase.com&#10;*.linkedin.com&#10;/^api\.example\.com$/"
        />
        <div className="rounded-md border border-zinc-200 bg-zinc-50 p-3 dark:border-zinc-800 dark:bg-zinc-900">
          <label className="text-xs font-medium text-zinc-700 dark:text-zinc-300">
            Test a destination
          </label>
          <div className="mt-1 flex gap-2">
            <Input
              type="text"
              value={form.tester}
              onChange={(e) => setForm({ ...form, tester: e.target.value })}
              placeholder="api.example.com"
              className="h-8 text-sm"
            />
            <BlocklistTester
              blocklist={form.blocklist}
              probe={form.tester}
            />
          </div>
        </div>
        <label className="flex items-center gap-3 text-sm">
          <input
            type="checkbox"
            checked={form.perCustomerEnabled}
            onChange={(e) =>
              setForm({ ...form, perCustomerEnabled: e.target.checked })
            }
            className="h-4 w-4 accent-emerald-600"
          />
          Rotate destinations after
          <Input
            type="number"
            min={1}
            max={240}
            value={form.perCustomerMinutes}
            onChange={(e) =>
              setForm({
                ...form,
                perCustomerMinutes: Number(e.target.value),
              })
            }
            disabled={!form.perCustomerEnabled}
            className="h-8 w-20 text-sm"
            aria-label="Per-customer minutes cap"
          />
          consecutive minutes per customer
        </label>
      </section>

      <div className="sticky bottom-0 flex items-center justify-end gap-2 border-t border-zinc-200 bg-white py-3 dark:border-zinc-800 dark:bg-zinc-950">
        <Button type="submit" disabled={saving} data-testid="save-button">
          {saving ? "Saving…" : "Save schedule"}
        </Button>
      </div>
    </form>
  );
}

function CapSlider({
  label,
  value,
  onChange,
  unit,
  max,
  used,
  usedFormat,
  capFormat,
  error,
}: {
  label: string;
  value: number;
  onChange: (n: number) => void;
  unit: string;
  max: number;
  used?: number;
  usedFormat?: (v: number) => string;
  capFormat: (v: number) => string;
  error?: string;
}) {
  return (
    <div>
      <div className="flex items-baseline justify-between text-sm">
        <label className="font-medium">{label}</label>
        <span className="font-mono text-xs text-zinc-600 dark:text-zinc-400">
          {capFormat(value)}
        </span>
      </div>
      <input
        type="range"
        min={0}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="mt-1 w-full accent-emerald-600"
        aria-label={label}
      />
      <div className="flex items-center justify-between text-xs text-zinc-500">
        <span>
          0{unit} ··· {max}
          {unit}
        </span>
        {used !== undefined && usedFormat ? (
          <span>
            {usedFormat(used)} used / {capFormat(value)} cap
          </span>
        ) : null}
      </div>
      {error ? <p className="mt-1 text-xs text-rose-600">{error}</p> : null}
    </div>
  );
}

function BlocklistTester({
  blocklist,
  probe,
}: {
  blocklist: string;
  probe: string;
}) {
  const matched = React.useMemo(() => {
    if (!probe) return null;
    const lines = blocklist
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter(Boolean);
    for (const pat of lines) {
      if (pat.startsWith("/") && pat.endsWith("/")) {
        try {
          if (new RegExp(pat.slice(1, -1), "i").test(probe)) return pat;
        } catch {
          continue;
        }
      } else if (pat.includes("*")) {
        const re = new RegExp(
          "^" +
            pat
              .replace(/[.+?^${}()|[\]\\]/g, "\\$&")
              .replace(/\*/g, ".*") +
            "$",
          "i",
        );
        if (re.test(probe)) return pat;
      } else if (pat.toLowerCase() === probe.toLowerCase()) {
        return pat;
      }
    }
    return null;
  }, [blocklist, probe]);

  if (!probe) {
    return (
      <span className="self-center text-xs text-zinc-500">
        Type a host to test.
      </span>
    );
  }
  return matched ? (
    <span
      data-testid="blocklist-match"
      className="self-center rounded-full bg-rose-100 px-2 py-0.5 text-xs font-medium text-rose-700 dark:bg-rose-950 dark:text-rose-300"
    >
      Blocked by <code>{matched}</code>
    </span>
  ) : (
    <span
      data-testid="blocklist-clean"
      className="self-center rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
    >
      Would be allowed
    </span>
  );
}
