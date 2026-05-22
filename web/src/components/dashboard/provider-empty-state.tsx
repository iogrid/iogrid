import Link from "next/link";

/**
 * ProviderEmptyState — shared CTA card rendered on every /provider/*
 * surface when the gateway-bff replies with `has_provider: false`. We
 * deliberately do NOT render the skeleton dashboard with em-dash
 * placeholders in that case: an operator with zero paired daemons needs
 * to be pointed at /install, not handed a misleading "all caps zero"
 * view that implies their machine is up but quiet. Issue #313.
 *
 * Surface-specific copy is supplied by the caller via `subtitle` so the
 * page contexts (overview / earnings / schedule / audit / staking) each
 * keep their own voice without forking the layout. The headline + CTA
 * stay identical so the operator always sees the same next-step.
 *
 * NOTE: this is a server-component-compatible presentational tile; it
 * has no client hooks. The gating check (`has_provider === false`) is
 * the caller's responsibility, performed inside whichever client island
 * owns the data fetch.
 */
export interface ProviderEmptyStateProps {
  /** Surface-specific paragraph rendered under the headline. */
  subtitle: string;
  /** Test selector for Playwright walks. Defaults per-surface in callers. */
  testId?: string;
}

export function ProviderEmptyState({
  subtitle,
  testId = "provider-empty-state",
}: ProviderEmptyStateProps) {
  return (
    <div
      data-testid={testId}
      className="rounded-md border border-dashed border-border-strong bg-muted p-8 text-center dark:border-border-strong dark:bg-card"
    >
      <p className="text-base font-semibold text-foreground dark:text-foreground">
        You don&apos;t have any provider machines paired yet.
      </p>
      <p className="mx-auto mt-2 max-w-prose text-sm text-muted-foreground dark:text-muted-foreground">
        {subtitle}
      </p>
      <div className="mt-5">
        <Link
          href="/install"
          className="inline-flex items-center gap-1 rounded-md bg-foreground px-4 py-2 text-sm font-medium text-background hover:bg-foreground/80 dark:bg-foreground dark:text-background dark:hover:bg-muted"
        >
          Install daemon
          <span aria-hidden>→</span>
        </Link>
      </div>
    </div>
  );
}

/** Canonical subtitle for the /provide overview page (issue #313). */
export const PROVIDER_EMPTY_OVERVIEW_SUBTITLE =
  "Install the iogrid daemon on a Mac, Linux, or Windows machine to start earning $GRID.";

/** Canonical subtitle for /provider/earnings (issue #313). */
export const PROVIDER_EMPTY_EARNINGS_SUBTITLE =
  "Earnings will appear here once a provider is paired and runs workloads.";

/** Canonical subtitle for /provider/schedule (issue #313). */
export const PROVIDER_EMPTY_SCHEDULE_SUBTITLE =
  "Pair a provider first to configure its caps, calendar, and category opt-ins.";

/** Canonical subtitle for /provider/audit (issue #313). */
export const PROVIDER_EMPTY_AUDIT_SUBTITLE =
  "Audit events will appear here once a provider is paired and starts accepting workloads.";

/** Canonical subtitle for /provider/staking (issue #313). */
export const PROVIDER_EMPTY_STAKING_SUBTITLE =
  "Stake $GRID against a paired provider to earn yield bonuses.";
