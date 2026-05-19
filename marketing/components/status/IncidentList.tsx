import type { Incident, IncidentImpact, IncidentStatus } from "./types";

interface Props {
  incidents: Incident[];
  emptyLabel: string;
}

const IMPACT_BORDER: Record<IncidentImpact, string> = {
  none: "border-neutral-200",
  minor: "border-warning/40",
  major: "border-warning/70",
  critical: "border-danger/60",
};

const IMPACT_BADGE: Record<IncidentImpact, string> = {
  none: "bg-neutral-100 text-neutral-600",
  minor: "bg-warning/10 text-warning",
  major: "bg-warning/15 text-warning",
  critical: "bg-danger/10 text-danger",
};

const STATUS_LABEL: Record<IncidentStatus, string> = {
  investigating: "Investigating",
  identified: "Identified",
  monitoring: "Monitoring",
  resolved: "Resolved",
};

function formatDate(iso: string) {
  try {
    const d = new Date(iso);
    return d.toLocaleString(undefined, {
      year: "numeric",
      month: "short",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      timeZoneName: "short",
    });
  } catch {
    return iso;
  }
}

export function IncidentList({ incidents, emptyLabel }: Props) {
  if (!incidents || incidents.length === 0) {
    return (
      <p className="card text-sm text-neutral-600">{emptyLabel}</p>
    );
  }
  return (
    <ol role="list" className="space-y-3">
      {incidents.map((inc) => (
        <li
          key={inc.id}
          className={`card border-l-4 ${IMPACT_BORDER[inc.impact] ?? IMPACT_BORDER.minor}`}
        >
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="h-card text-lg">{inc.title}</p>
              <p className="text-xs text-neutral-500">
                Started {formatDate(inc.started_at)}
                {inc.resolved_at ? ` · Resolved ${formatDate(inc.resolved_at)}` : ""}
              </p>
            </div>
            <div className="flex shrink-0 flex-wrap items-center gap-2">
              <span
                className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${IMPACT_BADGE[inc.impact] ?? IMPACT_BADGE.minor}`}
                aria-label={`Impact: ${inc.impact}`}
              >
                {inc.impact}
              </span>
              <span className="rounded-full bg-neutral-100 px-2.5 py-0.5 text-xs font-semibold text-neutral-700">
                {STATUS_LABEL[inc.status] ?? inc.status}
              </span>
            </div>
          </div>

          {inc.affected_services && inc.affected_services.length > 0 ? (
            <p className="mt-2 text-xs text-neutral-500 font-mono">
              affects: {inc.affected_services.join(", ")}
            </p>
          ) : null}

          {inc.updates && inc.updates.length > 0 ? (
            <ol
              role="list"
              className="mt-4 space-y-3 border-l border-neutral-200 pl-4"
            >
              {inc.updates.map((u) => (
                <li key={u.id} className="text-sm">
                  <p className="text-xs text-neutral-500">
                    <span className="font-semibold text-neutral-700">
                      {STATUS_LABEL[u.status] ?? u.status}
                    </span>
                    {" · "}
                    {formatDate(u.created_at)}
                  </p>
                  <p className="mt-1 text-neutral-700">{u.body}</p>
                </li>
              ))}
            </ol>
          ) : inc.body ? (
            <p className="mt-3 text-sm text-neutral-700">{inc.body}</p>
          ) : null}
        </li>
      ))}
    </ol>
  );
}
