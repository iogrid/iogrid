/**
 * TypeScript shapes for the JSON envelopes the gateway-bff emits to the
 * admin app. Slimmed copy of `web/src/lib/types.ts` — only the surfaces
 * the staff console actually consumes (abuse rules + provider summaries).
 *
 * Keep this file in lockstep with `web/src/lib/types.ts` for the shared
 * types; the admin app must not drift from the BFF contract the web app
 * also uses.
 */

export interface UUIDValue {
  value: string;
}

/**
 * proto3 `google.protobuf.Timestamp` as marshalled by Go's
 * `encoding/json` (the path the BFF uses today — NOT `protojson`).
 */
export type ProtoTimestamp = { seconds?: string | number; nanos?: number };

/**
 * AbuseFilterRule mirrors `iogrid.antiabuse.v1.FilterRule`. snake_case
 * matches the proto-generated JSON. `last_updated_at` may arrive as
 * either an RFC3339 string (future protojson) or the stdlib-encoded
 * `*timestamppb.Timestamp` struct {seconds, nanos} (#304).
 */
export interface AbuseFilterRule {
  id: string;
  slug: string;
  description: string;
  version: string;
  last_updated_at?: string | ProtoTimestamp | null;
}

export interface ListFiltersResponse {
  rules?: AbuseFilterRule[];
  ruleset_hash?: string;
}

/**
 * Provider summary used by the audit lookup + paired-providers table.
 * Sourced from `iogrid.providers.v1.Provider` via gateway-bff's
 * encoding/json (not protojson — fields are camelCase or snake_case
 * depending on the Go struct tags; we accept either spelling).
 */
export interface ProviderSummary {
  id?: UUIDValue;
  ownerUserId?: UUIDValue;
  displayName?: string;
  status?: string;
  registeredAt?: string;
  lastSeenAt?: string;
  hostInfo?: { os?: string; arch?: string; hostname?: string };
}

/**
 * Audit event over the SSE transparency stream. Trimmed to the fields
 * the admin lookup card renders; the full shape lives in
 * `web/src/lib/types.ts` for the provider-facing dashboard.
 */
export interface AuditEvent {
  id?: UUIDValue;
  providerId?: UUIDValue;
  kind?: string | number;
  occurredAt?: string;
  workloadType?: string | number;
  category?: string;
  customerDisplayName?: string;
  destinationSummary?: string;
  bytes?: string;
  metadata?: Record<string, string>;
}
