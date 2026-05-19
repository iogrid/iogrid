import type { ServicePosture, ServiceStatus } from "./types";

interface Props {
  services: ServicePosture[];
}

const STATUS_PILL: Record<
  ServiceStatus,
  { label: string; bg: string; text: string; dot: string }
> = {
  up: { label: "Operational", bg: "bg-success/10", text: "text-success", dot: "bg-success" },
  degraded: {
    label: "Degraded",
    bg: "bg-warning/10",
    text: "text-warning",
    dot: "bg-warning",
  },
  down: { label: "Outage", bg: "bg-danger/10", text: "text-danger", dot: "bg-danger" },
};

// Friendly display names. Falls back to the raw service slug when not
// listed — keeps the page robust against new services landing before
// this map gets updated.
const DISPLAY_NAMES: Record<string, string> = {
  "proxy-gateway": "Bandwidth proxy",
  "build-gateway": "iOS build CI",
  "identity-svc": "Identity & auth",
  "workloads-svc": "Workload dispatch",
  "billing-svc": "Billing & payouts",
  "vpn-gateway": "Consumer VPN",
  "providers-svc": "Provider directory",
  "antiabuse-svc": "Anti-abuse pipeline",
  "telemetry-svc": "Telemetry & SLOs",
  "gateway-bff": "Management plane API",
};

function displayName(slug: string) {
  return DISPLAY_NAMES[slug] ?? slug;
}

export function ServiceGrid({ services }: Props) {
  if (services.length === 0) {
    return (
      <p className="card text-sm text-neutral-600">
        SLO posture for individual services will appear here once the
        telemetry-svc backend reports its catalogue. The headline above
        still reflects current incident state.
      </p>
    );
  }
  return (
    <ul
      role="list"
      className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3"
    >
      {services.map((s) => {
        const pill = STATUS_PILL[s.status];
        return (
          <li
            key={s.name}
            className="card flex items-center justify-between gap-3"
          >
            <div className="min-w-0">
              <p className="h-card text-base truncate">{displayName(s.name)}</p>
              <p className="text-xs text-neutral-500 font-mono truncate">
                {s.name}
              </p>
            </div>
            <span
              className={`inline-flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1 text-xs font-semibold ${pill.bg} ${pill.text}`}
              aria-label={`Status: ${pill.label}`}
            >
              <span
                aria-hidden="true"
                className={`h-2 w-2 rounded-full ${pill.dot}`}
              />
              {pill.label}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
