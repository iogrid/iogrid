// Wire types for the iogrid customer API.
//
// These mirror proto/gen/openapi/iogrid.yaml verbatim. JSON shape is the
// source of truth (humans read it, fetch reads it, anti-CSAM filters
// read it). The proto contracts in proto/iogrid/**/v1/*.proto are the
// internal source of truth and the OpenAPI spec is generated from them.
//
// If you need to add a field, add it to the proto first, regenerate the
// OpenAPI spec, then mirror it here. Drift between this file and the
// OpenAPI spec is caught by the contract tests in test/contract.test.ts.

export type WorkloadType = 'BANDWIDTH' | 'DOCKER' | 'GPU' | 'IOS_BUILD';
export type WorkloadPriority = 'LOW' | 'NORMAL' | 'HIGH';

export interface Money {
  /** ISO 4217 currency code (e.g. "USD"). */
  currency: string;
  /** Amount in millionths of the major currency unit. 12.34 USD == 12_340_000. */
  micros: number;
}

export interface BandwidthRequest {
  targetUrl: string;
  method?: string;
  sessionId?: string;
  preferredRegion?: string;
  category?: string;
  maxSpend?: Money;
}

export interface DockerRequest {
  image: string;
  command?: string[];
  env?: Record<string, string>;
  /** Hard wall-clock timeout in seconds. */
  timeoutSeconds?: number;
  minCpuCores?: number;
  minMemoryMib?: number;
  minGpuMemoryMib?: number;
}

export interface GpuRequest {
  image: string;
  command?: string[];
  env?: Record<string, string>;
  timeoutSeconds?: number;
  minVramMib?: number;
  allowedVendors?: string[];
}

export interface IosBuildRequest {
  sourceTarballS3Key: string;
  tartImage: string;
  buildCommands?: string[];
  artifactS3Bucket?: string;
  artifactS3Prefix?: string;
}

export interface CreateWorkloadRequest {
  type: WorkloadType;
  priority?: WorkloadPriority;
  labels?: Record<string, string>;
  bandwidth?: BandwidthRequest;
  docker?: DockerRequest;
  gpu?: GpuRequest;
  iosBuild?: IosBuildRequest;
}

export interface Workload {
  id: string;
  workspaceId: string;
  submittedByUserId?: string;
  type: WorkloadType;
  priority?: WorkloadPriority;
  status: string;
  submittedAt?: string;
  startedAt?: string;
  finishedAt?: string;
  labels?: Record<string, string>;
  bandwidth?: BandwidthRequest;
  docker?: DockerRequest;
  gpu?: GpuRequest;
  iosBuild?: IosBuildRequest;
}

export interface WorkloadResult {
  workloadId: string;
  terminalStatus: string;
  exitCode?: number;
  logsS3Key?: string;
  bytesIn?: number;
  bytesOut?: number;
  artifactS3Keys?: string[];
  cost?: Money;
  completedAt?: string;
}

export interface GetWorkloadResponse {
  workload: Workload;
  result?: WorkloadResult;
}

export interface WorkloadEvent {
  workloadId: string;
  newStatus: string;
  occurredAt: string;
  note?: string;
}

export interface ListWorkloadsOptions {
  pageSize?: number;
  pageToken?: string;
  type?: WorkloadType;
  status?: string;
  submittedAfter?: string;
  submittedBefore?: string;
}

export interface ListWorkloadsResponse {
  workloads: Workload[];
  nextPageToken?: string;
}

export interface CreateApiKeyRequest {
  name: string;
  expiresAt?: string;
  scopes?: string[];
}

export interface ApiKeyMetadata {
  id: string;
  name: string;
  prefix: string;
  createdAt: string;
  lastUsedAt?: string;
  expiresAt?: string;
  scopes?: string[];
}

export interface CreatedApiKey extends ApiKeyMetadata {
  /** Only returned at creation time. Store securely. */
  secret: string;
}

export interface ListApiKeysResponse {
  keys: ApiKeyMetadata[];
  nextPageToken?: string;
}

export interface UsageRecord {
  id: string;
  workloadId: string;
  type: WorkloadType;
  quantity: number;
  cost: Money;
  recordedAt: string;
}

export interface GetUsageOptions {
  pageSize?: number;
  pageToken?: string;
  type?: WorkloadType;
  windowStart?: string;
  windowEnd?: string;
}

export interface ListUsageResponse {
  usage: UsageRecord[];
  nextPageToken?: string;
  pageSubtotal?: Money;
}

export interface Invoice {
  id: string;
  periodStart?: string;
  periodEnd?: string;
  subtotal?: Money;
  tax?: Money;
  total: Money;
  status: 'draft' | 'open' | 'paid' | 'void' | 'uncollectible';
  issuedAt?: string;
  paidAt?: string;
  hostedInvoiceUrl?: string;
}

export interface GetInvoicesOptions {
  pageSize?: number;
  pageToken?: string;
}

export interface ListInvoicesResponse {
  invoices: Invoice[];
  nextPageToken?: string;
}

// --- Mobile VPN session bring-up (POST /v1/vpn/sessions/mobile) -----------
//
// Wire shapes for the mobile-app one-shot session endpoint. Returns the
// full WireGuard peer config in a single round-trip so the iOS/Android
// PacketTunnelProvider can call WireGuardAdapter.start without a second
// hop. Distinct from the legacy daemon-driven flow at POST /v1/vpn/sessions.

/** Quota gate signal echoed on every mobile response (#573). */
export type QuotaState =
  | 'QUOTA_STATE_UNSPECIFIED'
  | 'QUOTA_STATE_HEALTHY'
  | 'QUOTA_STATE_THROTTLED'
  | 'QUOTA_STATE_EXHAUSTED';

/**
 * Request payload for POST /v1/vpn/sessions/mobile. NOTE: the VPN
 * surface uses snake_case on the wire (distinct from the workload /
 * billing surfaces which use camelCase) — the JSON keys match the
 * vpn-svc handler verbatim.
 */
export interface RequestMobileSessionRequest {
  /** Customer UUID — anchors the session row + quota counter. */
  customer_id: string;
  /** Region code or "auto" (default) for geo-nearest pick. */
  region?: string;
  /** Customer WireGuard public key (base64). */
  client_public_key: string;
  /** Optional API key — required only when the server enforces validation. */
  api_key?: string;
  /**
   * Optional Track-5 payment authorization blob. Opaque to the SDK;
   * vpn-svc persists it verbatim and validates downstream.
   */
  payment_authorization?: unknown;
}

/** Response from POST /v1/vpn/sessions/mobile (snake_case wire). */
export interface RequestMobileSessionResponse {
  session_id: string;
  peer_public_key: string;
  /** dotted-quad+port the mobile client passes to WireGuardKit. */
  peer_endpoint: string;
  /** customer-side inner CIDR (e.g. "10.244.7.4/32"). */
  customer_inner_cidr: string;
  /** allowed_ips on the WG peer (e.g. "0.0.0.0/0"). */
  allowed_ips: string;
  /** DNS resolvers to set on the tunnel interface. */
  dns_servers: string[];
  /** Resolved region (echoes back even when caller asked for "auto"). */
  region: string;
  /** RFC3339 timestamp when the session expires. */
  expires_at: string;
  /** Quota gate state for the mobile banner. */
  quota_state: QuotaState | string;
}

/** Mirror of iogrid.common.v1.ErrorCode (only customer-relevant codes). */
export type ErrorCode =
  | 'INVALID_ARGUMENT'
  | 'NOT_FOUND'
  | 'ALREADY_EXISTS'
  | 'PERMISSION_DENIED'
  | 'UNAUTHENTICATED'
  | 'RESOURCE_EXHAUSTED'
  | 'FAILED_PRECONDITION'
  | 'INTERNAL'
  | 'UNAVAILABLE'
  | 'DEADLINE_EXCEEDED'
  | 'ABUSE_BLOCKED'
  | 'ABUSE_RATE_LIMITED'
  | 'ABUSE_CATEGORY_DISALLOWED'
  | 'ABUSE_DESTINATION_BLOCKED'
  | 'STEP_UP_REQUIRED'
  | 'BILLING_PAST_DUE';

export interface ErrorEnvelope {
  code: ErrorCode | string;
  message: string;
  fieldPath?: string;
  metadata?: Record<string, string>;
  requestId?: string;
}
