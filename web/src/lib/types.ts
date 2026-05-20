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

// ---- billing-svc earnings summary (#324) ----------------------------------
// Distinct from EarningsSummary above: that one (providers-svc) breaks
// revenue down by workload_type for a TimeWindow; this one (billing-svc)
// is the headline-card aggregation the /provide/earnings page reads at
// the top — lifetime / last-30d / last-7d / pending / workload count.

export interface BillingEarningsSummary {
  providerId?: UUIDValue;
  totalEarned?: Money;
  /** Trailing 30 days. proto field name: last_30d. */
  last30D?: Money;
  /** Trailing 7 days. proto field name: last_7d. */
  last7D?: Money;
  /** Credited but not yet swept by the off-ramp cron. */
  pendingPayout?: Money;
  /** int64 → JSON-canonical string. */
  lifetimeWorkloads?: string | number;
  computedAt?: string;
}

export interface BillingGetEarningsSummaryResponse {
  summary?: BillingEarningsSummary;
}

export type PayoutMethodKind =
  | "PAYOUT_METHOD_KIND_UNSPECIFIED"
  | "PAYOUT_METHOD_KIND_CASH_USDC"
  | "PAYOUT_METHOD_KIND_FREE_VPN"
  | "PAYOUT_METHOD_KIND_CHARITY";

export interface PayoutMethod {
  userId?: UUIDValue;
  kind: PayoutMethodKind;
  /** Solana wallet for CASH_USDC; empty otherwise. */
  destinationAddress?: string;
  /** Opaque charity id for CHARITY; empty otherwise. */
  charityId?: string;
  updatedAt?: string;
}

export interface GetPayoutMethodResponse {
  method?: PayoutMethod;
}

export interface SetPayoutMethodResponse {
  method?: PayoutMethod;
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

export interface ProviderDashboard {
  earnings?: GetEarningsSummaryResponse;
  state?: GetCurrentStateResponse;
  recent_events?: AuditEvent[];
}

export interface GetSchedulingConfigResponse {
  config?: SchedulingConfig;
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
export type ProtoTimestamp = { seconds?: string | number; nanos?: number };

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
