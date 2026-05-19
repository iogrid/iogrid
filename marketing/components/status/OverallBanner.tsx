import type { Overall } from "./types";

interface Props {
  overall: Overall;
}

// Tailwind cannot statically derive these classes from a runtime
// expression, so we maintain an explicit map. The colour tokens here
// match brand/tokens/colors.json (success / warning / danger).
const VARIANTS: Record<
  Overall,
  { bg: string; border: string; ring: string; headline: string; sub: string; dot: string }
> = {
  up: {
    bg: "bg-success/10",
    border: "border-success/30",
    ring: "ring-success/20",
    headline: "All systems operational",
    sub: "Every iogrid service is within SLO budget.",
    dot: "bg-success",
  },
  degraded: {
    bg: "bg-warning/10",
    border: "border-warning/30",
    ring: "ring-warning/20",
    headline: "Partial service degradation",
    sub: "One or more services are burning SLO budget. Workloads may run slower than usual.",
    dot: "bg-warning",
  },
  down: {
    bg: "bg-danger/10",
    border: "border-danger/30",
    ring: "ring-danger/20",
    headline: "Major outage",
    sub: "One or more services are unavailable. The on-call team is engaged.",
    dot: "bg-danger",
  },
};

export function OverallBanner({ overall }: Props) {
  const v = VARIANTS[overall];
  return (
    <div
      role="status"
      aria-live="polite"
      className={`flex items-center gap-4 rounded-xl border ${v.border} ${v.bg} px-6 py-5 ring-1 ${v.ring}`}
    >
      <span
        aria-hidden="true"
        className={`inline-block h-3 w-3 rounded-full ${v.dot}`}
      />
      <div>
        <p className="h-card text-lg">{v.headline}</p>
        <p className="text-sm text-neutral-600">{v.sub}</p>
      </div>
    </div>
  );
}
