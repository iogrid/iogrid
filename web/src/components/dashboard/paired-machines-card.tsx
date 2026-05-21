"use client";

import * as React from "react";
import {
  formatProtoTimestampAbsolute,
  formatProtoTimestampRelative,
  truncateMiddle,
} from "@/lib/format";
import { cn } from "@/lib/utils";
import type { ProviderRef } from "@/lib/types";

/**
 * PairedMachinesCard — renders the "Paired machines" panel on /provide.
 *
 * Each entry shows the paired daemon's identity (display_name, status,
 * truncated id), liveness ("Last seen 2m ago"), enrolment date
 * ("Registered Mar 14, 2026"), and — once the daemon populates it —
 * the host platform/architecture. host_info fields are optional:
 * the card shape stays the same once the daemon starts wiring them
 * up, no UI churn required (#318).
 *
 * The card is rendered above the KPI strip so the operator sees
 * "Hatice's Mac" before the scheduler / earnings tiles — addressing
 * the founder DoD bar on EPIC #309 ("hatice signs in → sees her
 * paired Mac").
 *
 * The caller decides when to render this card. If `providers` is
 * empty / null / undefined the component returns `null` so the page
 * layout reflows cleanly — the install-CTA empty-state lives on a
 * separate path (PR #316 / #313) and we do NOT want to double-render
 * the empty case here.
 */
export interface PairedMachinesCardProps {
  providers: ProviderRef[] | null | undefined;
  /** Override "now" for deterministic tests. */
  nowMs?: number;
  className?: string;
}

export function PairedMachinesCard({
  providers,
  nowMs,
  className,
}: PairedMachinesCardProps) {
  if (!providers || providers.length === 0) return null;

  return (
    <section
      data-testid="paired-machines-card"
      aria-label="Paired machines"
      className={cn(
        "rounded-lg border border-border bg-card shadow-sm dark:border-border",
        className,
      )}
    >
      <header className="flex items-center justify-between border-b border-border px-5 py-3 dark:border-border">
        <h2 className="text-sm font-semibold text-foreground dark:text-foreground">
          Paired machines
        </h2>
        <span className="text-xs text-muted-foreground dark:text-muted-foreground">
          {providers.length === 1
            ? "1 daemon"
            : `${providers.length} daemons`}
        </span>
      </header>
      <ul className="divide-y divide-border dark:divide-border">
        {providers.map((p, i) => (
          <PairedMachineRow
            key={p.id?.value ?? `provider-${i}`}
            provider={p}
            nowMs={nowMs}
          />
        ))}
      </ul>
    </section>
  );
}

function PairedMachineRow({
  provider,
  nowMs,
}: {
  provider: ProviderRef;
  nowMs?: number;
}) {
  const name = provider.display_name?.trim() || "Unnamed daemon";
  const idValue = provider.id?.value ?? "";
  const platformLine = formatHostInfoLine(provider);

  return (
    <li className="flex flex-col gap-1 px-5 py-4">
      <div className="flex items-center justify-between gap-3">
        <p
          className="text-base font-semibold text-foreground dark:text-foreground"
          data-testid="paired-machine-name"
        >
          {name}
        </p>
        <ProviderStatusBadge status={provider.status} />
      </div>
      {idValue ? (
        <p
          className="font-mono text-xs text-muted-foreground dark:text-muted-foreground"
          data-testid="paired-machine-id"
          title={idValue}
        >
          {truncateMiddle(idValue, 8, 4)}
        </p>
      ) : null}
      <p className="text-xs text-muted-foreground dark:text-muted-foreground">
        Last seen{" "}
        <span data-testid="paired-machine-last-seen">
          {formatProtoTimestampRelative(provider.last_seen_at, nowMs)}
        </span>
      </p>
      <p className="text-xs text-muted-foreground dark:text-muted-foreground">
        Registered{" "}
        <span data-testid="paired-machine-registered">
          {formatProtoTimestampAbsolute(provider.registered_at)}
        </span>
      </p>
      {platformLine ? (
        <p
          className="text-xs text-muted-foreground dark:text-muted-foreground"
          data-testid="paired-machine-platform"
        >
          {platformLine}
        </p>
      ) : null}
    </li>
  );
}

/**
 * Decode the `host_info.platform` / `host_info.architecture` enums.
 * Both arrive as either the string enum name (when BFF eventually
 * switches to protojson) or the numeric int32 (today, via Go's
 * encoding/json). Returns `null` when neither field is populated so
 * the caller omits the row entirely — per #318 we never render
 * "Unknown" placeholders.
 */
function formatHostInfoLine(provider: ProviderRef): string | null {
  const host = provider.host_info;
  if (!host) return null;
  const platform = decodePlatform(host.platform);
  const arch = decodeArchitecture(host.architecture);
  if (!platform && !arch) return null;
  if (platform && arch) return `${platform} · ${arch}`;
  return platform ?? arch ?? null;
}

function decodePlatform(value: string | number | undefined): string | null {
  if (value === undefined || value === null) return null;
  if (typeof value === "string") {
    if (!value || value === "PLATFORM_UNSPECIFIED") return null;
    return humaniseEnum(value, "PLATFORM_");
  }
  // Numeric mapping mirrors iogrid.providers.v1.Platform (declaration
  // order in the proto). 0 = UNSPECIFIED → suppress.
  const numericMap: Record<number, string> = {
    1: "macOS",
    2: "Linux",
    3: "Windows",
  };
  return numericMap[value] ?? null;
}

function decodeArchitecture(
  value: string | number | undefined,
): string | null {
  if (value === undefined || value === null) return null;
  if (typeof value === "string") {
    if (!value || value === "ARCHITECTURE_UNSPECIFIED") return null;
    return humaniseEnum(value, "ARCHITECTURE_");
  }
  const numericMap: Record<number, string> = {
    1: "amd64",
    2: "arm64",
  };
  return numericMap[value] ?? null;
}

function humaniseEnum(raw: string, prefix: string): string {
  const trimmed = raw.startsWith(prefix) ? raw.slice(prefix.length) : raw;
  // "MAC_OS" → "Mac Os" — but for our known enums pick a cleaner
  // display string up front. Falls through to title-case otherwise.
  switch (trimmed) {
    case "MAC_OS":
    case "MACOS":
      return "macOS";
    case "LINUX":
      return "Linux";
    case "WINDOWS":
      return "Windows";
    case "AMD64":
    case "X86_64":
      return "amd64";
    case "ARM64":
    case "AARCH64":
      return "arm64";
    default:
      return trimmed
        .toLowerCase()
        .split("_")
        .map((s) => (s ? s[0].toUpperCase() + s.slice(1) : s))
        .join(" ");
  }
}

interface BadgeShape {
  label: string;
  tone: "active" | "idle" | "offline" | "neutral";
}

/**
 * ProviderStatus enum values (proto declaration order):
 *   0 = UNSPECIFIED
 *   1 = ACTIVE
 *   2 = OFFLINE
 *   3 = SUSPENDED
 *   4 = DEACTIVATED
 *
 * The BFF marshals via `encoding/json` over the generated Go enum
 * (int32), so today the wire carries a number. We also accept the
 * string enum name for a future protojson cutover.
 */
function ProviderStatusBadge({ status }: { status?: number | string }) {
  const shape = decodeStatus(status);
  const palette = {
    active:
      "bg-success/15 text-success dark:bg-success/15 dark:text-success",
    idle: "bg-warning/15 text-warning dark:bg-warning/15 dark:text-warning",
    offline: "bg-destructive/15 text-destructive dark:bg-destructive/15 dark:text-destructive",
    neutral: "bg-muted text-foreground dark:bg-muted dark:text-muted-foreground",
  } as const;
  return (
    <span
      data-testid="paired-machine-status"
      data-status={shape.tone}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold",
        palette[shape.tone],
      )}
    >
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full",
          shape.tone === "active"
            ? "bg-success"
            : shape.tone === "idle"
              ? "bg-warning"
              : shape.tone === "offline"
                ? "bg-destructive"
                : "bg-muted-foreground",
        )}
        aria-hidden
      />
      {shape.label}
    </span>
  );
}

function decodeStatus(status: number | string | undefined): BadgeShape {
  if (status === 1 || status === "PROVIDER_STATUS_ACTIVE") {
    return { label: "Active", tone: "active" };
  }
  if (status === 2 || status === "PROVIDER_STATUS_OFFLINE") {
    return { label: "Offline", tone: "offline" };
  }
  if (status === 3 || status === "PROVIDER_STATUS_SUSPENDED") {
    return { label: "Suspended", tone: "offline" };
  }
  if (status === 4 || status === "PROVIDER_STATUS_DEACTIVATED") {
    return { label: "Deactivated", tone: "neutral" };
  }
  // 0 / UNSPECIFIED / unknown — show a neutral "Pairing" pill rather
  // than masking the row, so the operator can see the daemon exists
  // but its status hasn't propagated yet.
  return { label: "Pairing", tone: "neutral" };
}
