/**
 * Lightweight TypeScript shapes for the JSON envelopes the gateway-bff
 * emits. We do NOT use the generated protobuf classes for the wire path
 * (they're large, runtime-dependent on @bufbuild/protobuf, and the BFF
 * serialises plain JSON anyway). These interfaces are derived from the
 * proto3 → JSON canonical mapping (camelCase field names, ISO-8601 for
 * timestamps, decimal-as-string for int64+, etc.).
 *
 * Keep this file the ONLY place where API JSON shapes are declared so a
 * BFF contract drift is fixed in one location.
 */

// ---- shared ---------------------------------------------------------------

export interface ApiError {
  code: string;
  message: string;
}

export interface UUIDValue {
  value: string;
}

export interface Money {
  /** ISO-4217 currency code, e.g. "USD". */
  currencyCode: string;
  /** Decimal string. proto3 maps int64 + scale to a string. */
  amount: string;
  /** Optional power-of-ten the amount is divided by (e.g. 2 → cents). */
  nanos?: number;
}

export interface TimeWindow {
  start?: string;
  end?: string;
}

/**
 * proto3 `google.protobuf.Timestamp` as marshalled by Go's
 * `encoding/json` (the path the BFF uses today — NOT `protojson`).
 * `seconds` is a uint64 that may arrive as either a number or a
 * decimal string depending on Go's marshaller. `seconds === 0` /
 * `"0"` / `undefined` means "never observed".
 */
export type ProtoTimestamp = { seconds?: string | number; nanos?: number };

// ---- identity / account ---------------------------------------------------

export interface Identifier {
  id?: UUIDValue;
  kind: "EMAIL" | "GOOGLE" | string;
  value: string;
  verified: boolean;
}

export interface User {
  id?: UUIDValue;
  displayName: string;
  primaryEmail: string;
  roles: string[];
  createdAt?: string;
}

export interface MeResponse {
  user: User;
  identifiers: Identifier[];
}

export interface Session {
  id?: UUIDValue;
  userAgent: string;
  ipAddress: string;
  createdAt: string;
  lastSeenAt: string;
  current: boolean;
}

export interface ListSessionsResponse {
  sessions: Session[];
}

// ---- provider dashboard ---------------------------------------------------

export type SchedulerState =
  | "ACTIVE"
  | "PAUSED_BANDWIDTH_CAP"
  | "PAUSED_CPU_CAP"
  | "PAUSED_MEMORY_CAP"
  | "PAUSED_OUTSIDE_CALENDAR"
  | "PAUSED_USER_ACTIVE"
  | "PAUSED_OPERATIONS"
  | "UNSPECIFIED";

export type EventKind =
  | "EVENT_KIND_WORKLOAD_DISPATCHED"
  | "EVENT_KIND_WORKLOAD_COMPLETED"
  | "EVENT_KIND_WORKLOAD_BLOCKED"
  | "EVENT_KIND_SCHEDULER_TRANSITION"
  | "EVENT_KIND_ABUSE_FLAGGED"
  | "EVENT_KIND_EARNINGS_CREDITED"
  | "EVENT_KIND_UNSPECIFIED";

export type WorkloadType =
  | "WORKLOAD_TYPE_BANDWIDTH"
  | "WORKLOAD_TYPE_DOCKER"
  | "WORKLOAD_TYPE_GPU"
  | "WORKLOAD_TYPE_IOS_BUILD"
  | "WORKLOAD_TYPE_UNSPECIFIED";

export interface AuditEvent {
  id?: UUIDValue;
  providerId?: UUIDValue;
  kind: EventKind;
  occurredAt?: string;
  workloadType: WorkloadType;
  category: string;
  customerDisplayName: string;
  destinationSummary: string;
  /** uint64 → JSON string per proto3 canonical mapping. */
  bytes: string;
  metadata?: Record<string, string>;
}

export interface EarningsSummary {
  providerId?: UUIDValue;
  window?: TimeWindow;
  totalEarned?: Money;
  byWorkloadType?: Record<string, Money>;
}

export interface GetEarningsSummaryResponse {
  summary?: EarningsSummary;
}

export interface ResourceCaps {
  bandwidthCapGbPerMonth: number;
  cpuCapPercent: number;
  memoryCapPercent: number;
  gpuCapPercentWhenIdle: number;
  gpuCapPercentWhenActive: number;
}

export interface CalendarWindow {
  /** ISO weekday, 1=Mon..7=Sun. proto3 enum DAY_OF_WEEK_MON etc. */
  dayOfWeek?: string;
  startLocalTime: string;
  endLocalTime: string;
  timezone: string;
}

export interface CalendarSchedule {
  windows: CalendarWindow[];
}

export interface IdleDetection {
  enabled: boolean;
  idleThresholdSeconds: number;
}

export interface CategoryOptIn {
  allowedCategories: string[];
  disallowedCategories: string[];
}

export interface DestinationPolicy {
  destinationBlocklist: string[];
  perCustomerMinutesCap: number;
}

export interface SchedulingConfig {
  providerId?: UUIDValue;
  caps?: ResourceCaps;
  calendar?: CalendarSchedule;
  idle?: IdleDetection;
  categoryOptIn?: CategoryOptIn;
  destinationPolicy?: DestinationPolicy;
  updatedAt?: string;
}

export interface CurrentUsageSnapshot {
  bandwidthUsedBytesThisMonth: string;
  cpuPercent: number;
  memoryPercent: number;
  gpuPercent: number;
  idleSeconds: number;
}

export interface GetCurrentStateResponse {
  state: SchedulerState;
  usage?: CurrentUsageSnapshot;
  reason?: string;
}

/**
 * HostInfo mirrors `iogrid.providers.v1.HostInfo`. Fields use the
 * snake_case names emitted by `encoding/json` over the generated Go
 * struct tags (matching the existing #298 / #304 convention). The
 * daemon does not yet populate any of these — see #318's "card shape
 * must be ready from day 1" — so every field is optional.
 */
export interface HostInfo {
  platform?: string | number;
  architecture?: string | number;
  os_version?: string;
  daemon_version?: string;
  total_memory_mib?: string | number;
  cpu_model?: string;
  cpu_logical_cores?: number;
  gpu_models?: string[];
  docker_available?: boolean;
  tart_available?: boolean;
}

/**
 * NetworkInfo mirrors `iogrid.providers.v1.NetworkInfo`.
 * inferred_region arrives as `{}` until the provider confirms.
 */
export interface NetworkInfo {
  public_ip?: string;
  asn?: number;
  isp?: string;
  throughput_mbps?: number;
  latency_ms?: number;
  inferred_region?: Record<string, unknown>;
}

/**
 * CapabilityInventory mirrors `iogrid.providers.v1.CapabilityInventory`.
 */
export interface CapabilityInventory {
  supported_workload_types?: string[];
  gpu_enabled?: boolean;
  ios_build_enabled?: boolean;
}

/**
 * ProviderRef mirrors `iogrid.providers.v1.Provider` projected onto
 * the BFF /provide/dashboard envelope. Numeric `status` matches the
 * `ProviderStatus` enum on the wire (proto3 int32 → JSON number when
 * marshalled via `encoding/json`, NOT `protojson`). Supersedes the
 * minimal {id, status} shape introduced in #316 — same field names,
 * just additional optional metadata for #318's "Paired machines" card.
 *
 * Timestamp fields arrive as `{seconds, nanos}` per the same `ProtoTimestamp`
 * convention used by AbuseFilterRule (#304). `seconds === 0`
 * indicates "never observed" — the daemon never checked in.
 */
export interface ProviderRef {
  id?: UUIDValue;
  owner_user_id?: UUIDValue;
  display_name?: string;
  /** ProviderStatus enum value (1=ACTIVE, 2=OFFLINE, 3=SUSPENDED, 4=DEACTIVATED, 0=UNSPECIFIED). */
  status?: number | string;
  host_info?: HostInfo | null;
  network_info?: NetworkInfo | null;
  capabilities?: CapabilityInventory | null;
  registered_at?: ProtoTimestamp | string | null;
  last_seen_at?: ProtoTimestamp | string | null;
}

export interface ProviderDashboard {
  earnings?: GetEarningsSummaryResponse;
  state?: GetCurrentStateResponse;
  recent_events?: AuditEvent[];
  /**
   * False when the caller owns zero paired providers. The web layer
   * MUST gate on this flag and render the "Install daemon" empty-state
   * instead of the skeleton dashboard with em-dash placeholders.
   * Backed by gateway-bff `providerDashboard.HasProvider` (issues #305
   * / #313). When true, `providers` carries the paired-machine
   * identities for the "Paired machines" card (#318) above the KPI strip.
   */
  has_provider?: boolean;
  providers?: ProviderRef[] | null;
}

export interface GetSchedulingConfigResponse {
  config?: SchedulingConfig;
  /**
   * False when the caller owns zero paired providers. Same gating
   * contract as ProviderDashboard.has_provider — UI renders the
   * "Install daemon" empty-state instead of the default form (#313).
   */
  has_provider?: boolean;
  providers?: ProviderRef[] | null;
}

export interface UpdateSchedulingConfigResponse {
  config?: SchedulingConfig;
}

// ---- customer -------------------------------------------------------------

export interface APIKey {
  id?: UUIDValue;
  workspaceId?: UUIDValue;
  label: string;
  prefix: string;
  createdAt: string;
  /** Plaintext is only returned on create. */
  plaintext?: string;
}

export interface ListAPIKeysResponse {
  keys: APIKey[];
}

export interface UsageRow {
  workloadType: WorkloadType;
  bytes: string;
  computeMillicpuSeconds: string;
  costMicros: string;
  bucketStart: string;
}

export interface ListUsageResponse {
  rows: UsageRow[];
}

// ---- VPN ------------------------------------------------------------------

export interface VPNAccount {
  tier: string;
  status: string;
  bandwidth_used_bytes: number;
  bandwidth_quota_bytes: number;
  upgrade_available: boolean;
}

export interface CheckoutSessionResponse {
  checkoutUrl: string;
  sessionId?: string;
}

// ---- admin ----------------------------------------------------------------

/**
 * AbuseFilterRule mirrors the proto-generated JSON shape
 * (`iogrid.antiabuse.v1.FilterRule`) produced by gateway-bff. Field
 * names follow Go's encoding/json snake_case tags emitted by
 * protoc-gen-go (NOT camelCase). See #298 — the previous typing
 * referenced ghost fields (`pattern`, `kind`, `reason`, `created_at`)
 * that the backend never populated, so every row rendered blank.
 *
 * `last_updated_at` may arrive as either an RFC3339 string (if the
 * upstream ever switches to `protojson`) or — as today — the
 * stdlib-encoded `*timestamppb.Timestamp` struct
 * `{seconds, nanos}` (#304). Renderers MUST normalise before display.
 */
export interface AbuseFilterRule {
  id: string;
  slug: string;
  description: string;
  version: string;
  last_updated_at?: string | ProtoTimestamp | null;
}

/**
 * ListFiltersResponse mirrors `iogrid.antiabuse.v1.ListFiltersResponse`.
 * Field names use snake_case to match the proto-generated JSON.
 */
export interface ListFiltersResponse {
  rules?: AbuseFilterRule[];
  ruleset_hash?: string;
}

// ---- auto-update (#59) ----------------------------------------------------

export type UpdateChannel = "stable" | "beta" | "canary";

export interface UpdatePreferences {
  channel: UpdateChannel;
  autoUpdate: boolean;
}

export type UpdateOutcome =
  | { status: "up_to_date"; current: string }
  | { status: "skipped"; reason: string }
  | { status: "staged"; from: string; to: string; path: string }
  | { status: "failed"; error: string };

export interface UpdateHistoryEntry {
  at: string;
  channel: string;
  fromVersion: string;
  outcome: UpdateOutcome;
}

export interface UpdateState {
  enabled: boolean;
  lastOutcome?: UpdateOutcome;
  pendingVersion?: string;
  history: UpdateHistoryEntry[];
}
