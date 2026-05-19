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
