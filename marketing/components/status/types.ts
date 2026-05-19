// Shared types for the status page. Mirror the JSON returned by
// telemetry-svc /status/posture and /status/uptime. Keep field names
// snake_case so the response can be assigned directly without a
// transform step.

export type Overall = "up" | "degraded" | "down";
export type ServiceStatus = "up" | "degraded" | "down";

export type IncidentImpact = "none" | "minor" | "major" | "critical";
export type IncidentStatus =
  | "investigating"
  | "identified"
  | "monitoring"
  | "resolved";

export interface ServicePosture {
  name: string;
  status: ServiceStatus;
  slo_percent: number;
}

export interface IncidentUpdate {
  id: string;
  incident_id: string;
  status: IncidentStatus;
  body: string;
  created_at: string;
}

export interface Incident {
  id: string;
  title: string;
  body?: string;
  status: IncidentStatus;
  impact: IncidentImpact;
  affected_services: string[];
  started_at: string;
  resolved_at?: string;
  created_at: string;
  updated_at: string;
  updates?: IncidentUpdate[];
}

export interface PostureResponse {
  schema_version: number;
  generated_at: string;
  overall: Overall;
  services: ServicePosture[];
  incidents_active: Incident[];
  incidents_recent: Incident[];
}

export interface UptimeSample {
  service: string;
  day: string; // YYYY-MM-DD
  state: "" | "op" | "deg" | "down" | "maint";
  sli_pct: number;
}

export interface UptimeResponse {
  schema_version: number;
  generated_at: string;
  service: string;
  days: number;
  samples: UptimeSample[];
}

// Shape of marketing/content/status/incidents-static.json. The on-call
// edits this file as a backstop for the case where /status/posture is
// itself unreachable (the status page should NEVER look broken because
// of its own backend going down).
export interface StaticIncidentsBundle {
  schema_version: number;
  comment?: string;
  active: Incident[];
  recent: Incident[];
}
