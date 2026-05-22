"use client";

import * as React from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { browserApi, ApiError } from "@/lib/api";
import { cn } from "@/lib/utils";

/**
 * Off-ramp ($GRID → fiat) drawer attached to /provider/earnings.
 *
 * Per issues #167 / #169 / #170:
 *
 *   - MoonPay is the default real implementation (loose-coupled,
 *     iogrid-controlled).
 *   - Sociable Cash is a documented contract stub (loose-coupled,
 *     cross-org; real impl lives at sociable-cloud/cash).
 *   - Coinbase is wired post-Wormhole-NTT bridge (Phase 2).
 *
 * The drawer:
 *   1. Lists registered providers from GET /api/v1/offramp/providers
 *   2. Lets the user pick amount + fiat currency + provider
 *   3. POSTs to /api/v1/offramp/start to mint a partner redirect URL
 *   4. Stores the request id in localStorage so we can show
 *      "Off-ramp in progress" on the earnings page across reloads.
 *   5. Redirects the browser to the partner's URL.
 */

const STORAGE_KEY = "iogrid_offramp_pending";

const GRID_DECIMALS = 9;
const LAMPORTS_PER_GRID = 10 ** GRID_DECIMALS;

const FIAT_OPTIONS = [
  { code: "USD", label: "USD — US Dollar" },
  { code: "EUR", label: "EUR — Euro" },
  { code: "GBP", label: "GBP — British Pound" },
  { code: "PHP", label: "PHP — Philippine Peso" },
  { code: "KES", label: "KES — Kenyan Shilling" },
];

// Display labels + status for each provider id. "coming-soon" providers
// render in the picker as disabled — the operator can opt them in at
// any time by extending OFFRAMP_PROVIDERS at the billing-svc.
type ProviderDescriptor = {
  name: string;
  label: string;
  description: string;
  status: "default" | "coming-soon";
};

const KNOWN_PROVIDERS: Record<string, ProviderDescriptor> = {
  moonpay: {
    name: "moonpay",
    label: "MoonPay",
    description:
      "Swap $GRID → USDC → bank transfer / debit card. KYC required. ~1% fee.",
    status: "default",
  },
  "sociable-cash": {
    name: "sociable-cash",
    label: "Sociable Cash",
    description:
      "Migrant-friendly off-ramp (GCash / M-Pesa / SEPA). Coming soon — contract integration in progress.",
    status: "coming-soon",
  },
  coinbase: {
    name: "coinbase",
    label: "Coinbase",
    description:
      "Off-ramp via Coinbase Pay (USDC → bank). Activated after the Wormhole NTT bridge to Base goes live.",
    status: "coming-soon",
  },
};

type ProvidersResponse = { providers: { name: string }[] };
type StartOffRampResponse = { request_id: string; redirect_url: string };

export type PendingOffRamp = {
  requestId: string;
  providerName: string;
  startedAt: string;
};

export function loadPendingOffRamps(): PendingOffRamp[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as PendingOffRamp[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function savePendingOffRamps(rows: PendingOffRamp[]) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(rows.slice(-10)));
  } catch {
    // localStorage unavailable (incognito quota) — silently drop.
  }
}

export function rememberOffRamp(p: PendingOffRamp) {
  const list = loadPendingOffRamps().filter((r) => r.requestId !== p.requestId);
  list.push(p);
  savePendingOffRamps(list);
}

export function forgetOffRamp(requestId: string) {
  savePendingOffRamps(
    loadPendingOffRamps().filter((r) => r.requestId !== requestId),
  );
}

/**
 * The drawer renders as a controlled component; the parent owns the
 * open/close state. Keeping it out of router state avoids leaking the
 * partner redirect destination into the URL bar.
 */
export function WithdrawDrawer({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [providers, setProviders] = React.useState<ProviderDescriptor[]>([]);
  const [providersLoading, setProvidersLoading] = React.useState(false);
  const [providerName, setProviderName] = React.useState<string>("moonpay");
  const [amount, setAmount] = React.useState<string>("");
  const [fiat, setFiat] = React.useState<string>("USD");
  const [submitting, setSubmitting] = React.useState(false);

  // Hydrate provider list from billing-svc — registered adapters appear
  // here, but we also surface KNOWN_PROVIDERS not yet enabled as
  // disabled options so the operator can preview the UX.
  React.useEffect(() => {
    if (!open) return;
    setProvidersLoading(true);
    browserApi()
      .get<ProvidersResponse>("/api/v1/offramp/providers")
      .then((res) => {
        const registered = new Set(res.providers.map((p) => p.name));
        const merged: ProviderDescriptor[] = Object.values(KNOWN_PROVIDERS).map(
          (p) => ({
            ...p,
            status: registered.has(p.name) ? "default" : p.status,
          }),
        );
        // Surface any provider the BFF reported that we don't know
        // about — defensive default.
        for (const r of res.providers) {
          if (!KNOWN_PROVIDERS[r.name]) {
            merged.push({
              name: r.name,
              label: r.name,
              description: "Off-ramp partner",
              status: "default",
            });
          }
        }
        setProviders(merged);
        // Default to the first available provider that isn't
        // coming-soon. Falls back to "moonpay" — the doc default.
        const firstAvailable = merged.find((p) => p.status === "default");
        if (firstAvailable) setProviderName(firstAvailable.name);
      })
      .catch((err) => {
        toast.error(`Failed to load off-ramp providers: ${err.message}`);
        setProviders(Object.values(KNOWN_PROVIDERS));
      })
      .finally(() => setProvidersLoading(false));
  }, [open]);

  const close = React.useCallback(() => {
    if (!submitting) onOpenChange(false);
  }, [onOpenChange, submitting]);

  // ESC closes the drawer.
  React.useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, close]);

  const submit = async () => {
    const amt = Number(amount);
    if (!Number.isFinite(amt) || amt <= 0) {
      toast.error("Enter a $GRID amount greater than 0.");
      return;
    }
    const lamports = Math.floor(amt * LAMPORTS_PER_GRID);
    if (lamports <= 0) {
      toast.error("Amount too small.");
      return;
    }
    const provider = providers.find((p) => p.name === providerName);
    if (provider && provider.status === "coming-soon") {
      toast.error(`${provider.label} is not enabled yet.`);
      return;
    }

    setSubmitting(true);
    try {
      const wallet = readBoundWallet();
      const res = await browserApi().post<StartOffRampResponse>(
        "/api/v1/offramp/start",
        {
          provider_name: providerName,
          wallet_address: wallet,
          grid_amount: lamports,
          fiat_currency: fiat,
          return_url:
            typeof window !== "undefined"
              ? window.location.origin + "/provider/earnings"
              : "",
        },
      );
      rememberOffRamp({
        requestId: res.request_id,
        providerName,
        startedAt: new Date().toISOString(),
      });
      toast.success(`Redirecting to ${providerName}…`);
      window.location.href = res.redirect_url;
    } catch (err) {
      const e = err as ApiError | Error;
      toast.error(`Off-ramp failed: ${e.message}`);
    } finally {
      setSubmitting(false);
    }
  };

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Withdraw earnings"
      className="fixed inset-0 z-50 flex"
    >
      <div
        className="flex-1 bg-foreground/10 dark:bg-background/80"
        onClick={close}
        aria-hidden="true"
      />
      <aside className="flex w-full max-w-md flex-col border-l border-border bg-background shadow-xl dark:border-border">
        <header className="flex items-center justify-between border-b border-border p-4 dark:border-border">
          <div>
            <h2 className="text-lg font-semibold">Withdraw to bank</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              Swap $GRID → fiat via a partner off-ramp.
            </p>
          </div>
          <button
            type="button"
            aria-label="Close"
            className="rounded-md p-1 text-muted-foreground hover:bg-muted dark:hover:bg-card"
            onClick={close}
            disabled={submitting}
          >
            ✕
          </button>
        </header>

        <div className="flex-1 space-y-5 overflow-y-auto p-4">
          <div>
            <label
              htmlFor="amount"
              className="block text-sm font-medium text-foreground dark:text-foreground"
            >
              Amount ($GRID)
            </label>
            <input
              id="amount"
              type="number"
              min="0"
              step="0.000000001"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="0.0"
              className="mt-1 w-full rounded-md border border-border-strong bg-card px-3 py-2 text-sm focus:border-muted-foreground focus:outline-none dark:border-border-strong"
            />
          </div>

          <div>
            <label
              htmlFor="fiat"
              className="block text-sm font-medium text-foreground dark:text-foreground"
            >
              Receive in
            </label>
            <select
              id="fiat"
              value={fiat}
              onChange={(e) => setFiat(e.target.value)}
              className="mt-1 w-full rounded-md border border-border-strong bg-card px-3 py-2 text-sm dark:border-border-strong"
            >
              {FIAT_OPTIONS.map((o) => (
                <option key={o.code} value={o.code}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <p className="text-sm font-medium text-foreground dark:text-foreground">
              Partner
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              Choose an off-ramp partner. Each handles KYC + swap + fiat
              settlement on its own platform.
            </p>
            <div className="mt-3 space-y-2">
              {providersLoading && providers.length === 0 ? (
                <div className="text-sm text-muted-foreground">Loading partners…</div>
              ) : (
                providers.map((p) => (
                  <ProviderOption
                    key={p.name}
                    provider={p}
                    selected={providerName === p.name}
                    onSelect={() =>
                      p.status === "default" && setProviderName(p.name)
                    }
                  />
                ))
              )}
            </div>
          </div>
        </div>

        <footer className="border-t border-border p-4 dark:border-border">
          <Button
            className="w-full"
            onClick={submit}
            disabled={submitting || !amount}
            aria-label="Continue to partner"
          >
            {submitting ? "Redirecting…" : "Continue to partner"}
          </Button>
          <p className="mt-2 text-xs text-muted-foreground">
            You will be redirected to the partner to complete KYC + receive
            funds. iogrid never custodies your $GRID.
          </p>
        </footer>
      </aside>
    </div>
  );
}

function ProviderOption({
  provider,
  selected,
  onSelect,
}: {
  provider: ProviderDescriptor;
  selected: boolean;
  onSelect: () => void;
}) {
  const disabled = provider.status === "coming-soon";
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      aria-disabled={disabled}
      disabled={disabled}
      className={cn(
        "w-full rounded-md border p-3 text-left transition-colors",
        selected && !disabled
          ? "border-success/40 bg-success/10 dark:bg-success/15"
          : "border-border bg-card hover:border-foreground/40 dark:border-border",
        disabled && "cursor-not-allowed opacity-60",
      )}
    >
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium">{provider.label}</span>
        {disabled ? (
          <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-semibold uppercase text-muted-foreground dark:bg-muted">
            Coming soon
          </span>
        ) : (
          <span className="rounded-full bg-success/15 px-2 py-0.5 text-[10px] font-semibold uppercase text-success dark:bg-success/15 dark:text-success">
            Available
          </span>
        )}
      </div>
      <p className="mt-1 text-xs text-muted-foreground dark:text-muted-foreground">
        {provider.description}
      </p>
    </button>
  );
}

/**
 * Read the user's bound Solana wallet from localStorage. The wallet
 * adapter saves the pubkey there at connect time. Falls back to an
 * empty string so the BFF returns a clear validation error.
 */
function readBoundWallet(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem("iogrid_wallet_address") ?? "";
  } catch {
    return "";
  }
}
