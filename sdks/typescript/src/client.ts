import { Transport, type FetchLike } from './transport.js';
import type {
  ApiKeyMetadata,
  CreateApiKeyRequest,
  CreateWorkloadRequest,
  CreatedApiKey,
  GetInvoicesOptions,
  GetUsageOptions,
  GetWorkloadResponse,
  Invoice,
  ListApiKeysResponse,
  ListInvoicesResponse,
  ListUsageResponse,
  ListWorkloadsOptions,
  ListWorkloadsResponse,
  RequestMobileSessionRequest,
  RequestMobileSessionResponse,
  UsageRecord,
  Workload,
  WorkloadEvent,
} from './types.js';

export interface IogridClientOptions {
  /** Customer API key (`iog_...`). Required. */
  apiKey: string;
  /**
   * Override the base URL. Defaults to https://api.iogrid.org. Set this
   * to https://api.staging.iogrid.org for staging.
   */
  baseUrl?: string;
  /** Per-request timeout in ms. Default 30000. Pass 0 to disable. */
  timeoutMs?: number;
  /** Custom fetch (for tests / Cloudflare Workers / Deno). */
  fetch?: FetchLike;
  /** Appended to the SDK's User-Agent header. */
  userAgent?: string;
}

const DEFAULT_BASE_URL = 'https://api.iogrid.org';
const SDK_VERSION = '0.1.0';

/**
 * IogridClient is the top-level customer SDK entry point.
 *
 * ```ts
 * const iogrid = new IogridClient({ apiKey: process.env.IOGRID_API_KEY! });
 * const w = await iogrid.createWorkload({
 *   type: 'BANDWIDTH',
 *   bandwidth: { targetUrl: 'https://example.com' },
 * });
 * console.log(w.id, w.status);
 * ```
 */
export class IogridClient {
  private readonly transport: Transport;

  constructor(opts: IogridClientOptions) {
    if (!opts.apiKey) {
      throw new Error('IogridClient: apiKey is required');
    }
    const fetchImpl: FetchLike =
      opts.fetch ??
      (typeof globalThis.fetch === 'function'
        ? (globalThis.fetch.bind(globalThis) as FetchLike)
        : (() => {
            throw new Error(
              'IogridClient: no global fetch; pass `fetch` in the constructor or run on Node 18.17+'
            );
          })());
    this.transport = new Transport({
      baseUrl: opts.baseUrl ?? DEFAULT_BASE_URL,
      apiKey: opts.apiKey,
      fetch: fetchImpl,
      timeoutMs: opts.timeoutMs ?? 30_000,
      userAgent: `iogrid-sdk-typescript/${SDK_VERSION}${opts.userAgent ? ` (${opts.userAgent})` : ''}`,
    });
  }

  // --- Workloads ----------------------------------------------------------

  /** Submit a new workload to the grid. */
  async createWorkload(
    body: CreateWorkloadRequest,
    signal?: AbortSignal
  ): Promise<Workload> {
    return this.transport.request<Workload>('POST', '/v1/workloads', body, undefined, signal);
  }

  /** Retrieve a workload by id (includes terminal result if finished). */
  async getWorkload(id: string, signal?: AbortSignal): Promise<GetWorkloadResponse> {
    return this.transport.request<GetWorkloadResponse>(
      'GET',
      `/v1/workloads/${encodeURIComponent(id)}`,
      undefined,
      undefined,
      signal
    );
  }

  /** List workloads in the caller's workspace. */
  async listWorkloads(
    opts: ListWorkloadsOptions = {},
    signal?: AbortSignal
  ): Promise<ListWorkloadsResponse> {
    return this.transport.request<ListWorkloadsResponse>(
      'GET',
      '/v1/workloads',
      undefined,
      {
        pageSize: opts.pageSize,
        pageToken: opts.pageToken,
        type: opts.type,
        status: opts.status,
        submittedAfter: opts.submittedAfter,
        submittedBefore: opts.submittedBefore,
      },
      signal
    );
  }

  /** Cancel a queued or running workload. */
  async cancelWorkload(
    id: string,
    reason?: string,
    signal?: AbortSignal
  ): Promise<Workload> {
    return this.transport.request<Workload>(
      'DELETE',
      `/v1/workloads/${encodeURIComponent(id)}`,
      undefined,
      { reason },
      signal
    );
  }

  /**
   * Stream workload events via Server-Sent Events. The returned
   * AsyncIterable yields each transition as a typed `WorkloadEvent`;
   * iteration completes when the workload reaches a terminal status.
   *
   * ```ts
   * for await (const ev of iogrid.streamWorkloadEvents(id)) {
   *   console.log(ev.newStatus, ev.note);
   * }
   * ```
   */
  streamWorkloadEvents(
    id: string,
    signal?: AbortSignal
  ): AsyncIterable<WorkloadEvent> {
    return this.transport.stream<WorkloadEvent>(
      `/v1/workloads/${encodeURIComponent(id)}/events`,
      undefined,
      signal
    );
  }

  // --- API keys -----------------------------------------------------------

  /**
   * Mint a new API key. The secret is returned ONLY in this response —
   * store it securely; subsequent list calls return only metadata.
   */
  async createApiKey(
    body: CreateApiKeyRequest,
    signal?: AbortSignal
  ): Promise<CreatedApiKey> {
    return this.transport.request<CreatedApiKey>('POST', '/v1/keys', body, undefined, signal);
  }

  /** List API keys for the caller's workspace (metadata only). */
  async listApiKeys(signal?: AbortSignal): Promise<ApiKeyMetadata[]> {
    const r = await this.transport.request<ListApiKeysResponse>(
      'GET',
      '/v1/keys',
      undefined,
      undefined,
      signal
    );
    return r.keys;
  }

  /** Revoke an API key. */
  async deleteApiKey(id: string, signal?: AbortSignal): Promise<void> {
    await this.transport.request<void>(
      'DELETE',
      `/v1/keys/${encodeURIComponent(id)}`,
      undefined,
      undefined,
      signal
    );
  }

  // --- Billing ------------------------------------------------------------

  /** Paged list of metered usage records. */
  async getUsage(
    opts: GetUsageOptions = {},
    signal?: AbortSignal
  ): Promise<UsageRecord[]> {
    const r = await this.transport.request<ListUsageResponse>(
      'GET',
      '/v1/usage',
      undefined,
      {
        pageSize: opts.pageSize,
        pageToken: opts.pageToken,
        type: opts.type,
        windowStart: opts.windowStart,
        windowEnd: opts.windowEnd,
      },
      signal
    );
    return r.usage;
  }

  /** Paged list of invoices issued against the caller's workspace. */
  async getInvoices(
    opts: GetInvoicesOptions = {},
    signal?: AbortSignal
  ): Promise<Invoice[]> {
    const r = await this.transport.request<ListInvoicesResponse>(
      'GET',
      '/v1/invoices',
      undefined,
      { pageSize: opts.pageSize, pageToken: opts.pageToken },
      signal
    );
    return r.invoices;
  }

  // --- Mobile VPN session bring-up ---------------------------------------

  /**
   * Request a one-shot mobile-app VPN session via POST
   * /v1/vpn/sessions/mobile. Returns the full WireGuard peer config so
   * the mobile PacketTunnelProvider can call WireGuardAdapter.start
   * without a second round-trip.
   *
   * Distinct from the legacy daemon-driven flow at POST
   * /v1/vpn/sessions. On 503 the SDK surfaces an IogridError whose
   * `metadata['retry-after']` carries the server's Retry-After hint —
   * use {@link retryAfterSeconds} to read it.
   *
   * ```ts
   * const s = await iogrid.requestMobileSession({
   *   customer_id: 'aaaa-bbbb-cccc-dddd',
   *   region: 'auto',
   *   client_public_key: 'base64-wg-public-key',
   * });
   * console.log(s.peer_endpoint, s.customer_inner_cidr);
   * ```
   */
  async requestMobileSession(
    body: RequestMobileSessionRequest,
    signal?: AbortSignal
  ): Promise<RequestMobileSessionResponse> {
    if (!body.customer_id) {
      throw new Error('requestMobileSession: customer_id is required');
    }
    if (!body.client_public_key) {
      throw new Error('requestMobileSession: client_public_key is required');
    }
    return this.transport.request<RequestMobileSessionResponse>(
      'POST',
      '/v1/vpn/sessions/mobile',
      body,
      undefined,
      signal
    );
  }
}
