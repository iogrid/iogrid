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

/**
 * Identifier — mirrors `iogrid.identity.v1.Identifier`. gateway-bff
 * serialises via stdlib `encoding/json` which emits proto enums as
 * numeric tags and snake_case field names. Both the proto/wire shape
 * AND the older camelCase shape are accepted so call sites can
 * migrate gradually. See #371 + #314.
 */
export interface Identifier {
  id?: UUIDValue;
  /**
   * Numeric proto enum tag (0–5; see `IdentifierKindNames` in
   * `proto-enum.ts`). Older code paths may receive the
   * SCREAMING_SNAKE_CASE string form; both are accepted.
   */
  kind: number | string;
  /** Presence indicates a verified email-bound identifier. */
  verified_email?: string;
  /** Provider subject (OAuth `sub`, Solana pubkey, …) — never an email. */
  subject?: string;
  registered_at?: { seconds?: string | number; nanos?: number } | string;
  last_used_at?: { seconds?: string | number; nanos?: number } | string;

  // Legacy fields (pre-#371 — kept to avoid breaking callers that
  // still read them; new code should use the proto/wire fields above).
  /** @deprecated use `verified_email` presence instead. */
  verified?: boolean;
  /** @deprecated use `verified_email` or `subject`. */
  value?: string;
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
  // gateway-bff returns the Connect-RPC JSON envelope of the canonical
  // identityv1.Session message, which uses snake_case field names per
  // the proto3-JSON mapping. We surface both spellings here so older
  // callers that referenced the camelCase aliases keep compiling
  // alongside the /account/sessions panel that consumes the canonical
  // names (issue #322).
  //
  // `id` carries the session UUID. Connect-RPC wraps it as
  // {value: "<uuid>"}; the older chi JSON twin emits a bare string —
  // both shapes are tolerated so the panel can pick whichever the
  // current call site returned.
  id?: UUIDValue | string;
  user_id?: UUIDValue | string;
  // Canonical protobuf-JSON fields (issue #322).
  user_agent?: string;
  ip_address?: string;
  created_at?: string;
  last_used_at?: string;
  expires_at?: string;
  is_current?: boolean;
  // Legacy camelCase aliases kept for backward compatibility with the
  // earlier hand-rolled JSON shape. The panel reads via the
  // sessionFieldShim helper which falls back across both.
  userAgent?: string;
  ipAddress?: string;
  createdAt?: string;
  lastSeenAt?: string;
  current?: boolean;
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
  // Internal stream-keepalive heartbeat (#323). Backed by
  // providers.v1.EventKind.EVENT_KIND_KEEPALIVE=7. The gateway-bff
  // SSE proxy drops these so they should never reach the browser,
  // but the union carries the variant so defence-in-depth code in
  // feed.tsx (and any future consumer) can switch on it safely.
  | "EVENT_KIND_KEEPALIVE"
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
  /**
   * gateway-bff serialises Connect-Go proto structs with `encoding/json`,
   * so enum fields arrive as numeric tags. The string-union form is
   * preserved for forwards-compatibility with proto3-JSON callers. See #314.
   */
  kind: EventKind | number;
  occurredAt?: string;
  workloadType: WorkloadType | number;
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
// is the headline-card aggregation the /provider/earnings page reads at
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
  /**
   * gateway-bff serialises Connect-Go proto structs with `encoding/json`,
   * which emits enums as numeric tags. We accept either form on the wire
   * and decode at the call site via `protoEnumName()`. See #314.
   */
  state: SchedulerState | number;
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
 * the BFF /provider/dashboard envelope. Numeric `status` matches the
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
  /**
   * Per-owner primary-daemon flag (#325). When the caller owns ≥2
   * paired daemons the schedule editor renders a picker that lets the
   * owner re-elect which is primary; non-primary rows show a "Set as
   * primary" button. At most one row per owner has is_primary=true
   * (enforced by a partial unique index in providers-svc).
   */
  is_primary?: boolean;
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
  /** See AuditEvent.kind — same JSON-wire caveat. */
  workloadType: WorkloadType | number;
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

// ---- prepaid $GRID balance (#632) -----------------------------------------

/**
 * CustomerBalance is the prepaid-balance read for /customer/billing.
 * Backed by gateway-bff GET /api/v1/customer/billing/balance, which
 * resolves the caller's bound wallet and reads billing-svc
 * /v1/grid/balance (on-chain $GRID + grace-overage arrears).
 *
 * Founder-ruled money model: prepaid + small capped grace overage. The
 * customer consumes only the $GRID they hold; `available_atomic` MAY dip
 * slightly negative — up to `grace_overage_cap_atomic` — and that arrears
 * (`grace_overage_owed_atomic`) MUST be cleared on the next top-up.
 *
 * Amounts are atomic (9-decimal) $GRID. `balance_grid` is a pre-rendered
 * decimal string convenience (up to 4 dp).
 */
export interface CustomerBalance {
  wallet: string;
  balance_atomic: number;
  balance_grid: string;
  grace_overage_owed_atomic: number;
  grace_overage_cap_atomic: number;
  available_atomic: number;
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

// ---- notification preferences (#631) --------------------------------------

// NotificationCategoryKey is the stable key persisted in the
// users.notification_prefs JSONB column. Keep these in sync with the
// NOTIFICATION_CATEGORIES table below — the keys are the contract
// between web + identity-svc (which treats the object as opaque JSON).
export type NotificationCategoryKey =
  | "earnings_credited"
  | "payout_sent"
  | "security_alerts"
  | "product_updates";

// NotificationChannelPrefs is the per-category channel toggle pair.
// `in_app` is snake_case to match the stored JSON keys verbatim (the
// payload is forwarded through gateway-bff untouched, so there is no
// camelCase remap layer for this surface).
export interface NotificationChannelPrefs {
  email: boolean;
  in_app: boolean;
}

// NotificationPrefs maps every category to its channel toggles.
export type NotificationPrefs = Record<
  NotificationCategoryKey,
  NotificationChannelPrefs
>;

// NOTIFICATION_CATEGORIES drives the /account/notifications table —
// label + helper copy for each persisted key.
export const NOTIFICATION_CATEGORIES: ReadonlyArray<{
  key: NotificationCategoryKey;
  label: string;
  description: string;
}> = [
  {
    key: "earnings_credited",
    label: "Earnings credited",
    description: "When your provider earnings are credited to your balance.",
  },
  {
    key: "payout_sent",
    label: "Payout sent",
    description: "When a payout leaves iogrid for your wallet or bank.",
  },
  {
    key: "security_alerts",
    label: "Security alerts",
    description: "New sign-ins, identifier changes, and account safety events.",
  },
  {
    key: "product_updates",
    label: "Product updates",
    description: "Occasional news about new iogrid features.",
  },
];

// defaultNotificationPrefs is the all-on-email baseline applied when the
// user has never saved a preference (the server returns null). Product
// updates default to in-app only so we don't email feature news without
// an explicit opt-in.
export const defaultNotificationPrefs: NotificationPrefs = {
  earnings_credited: { email: true, in_app: true },
  payout_sent: { email: true, in_app: true },
  security_alerts: { email: true, in_app: true },
  product_updates: { email: false, in_app: true },
};
