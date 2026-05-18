import type { ErrorCode, ErrorEnvelope } from './types.js';

/**
 * IogridError is the canonical error thrown by the SDK on non-2xx HTTP
 * responses. The `code` field is the stable machine-readable error code
 * (mirrors `iogrid.common.v1.ErrorCode`); callers should switch on
 * `code` rather than parsing the human message.
 *
 * Network failures (DNS, connection refused, abort) bubble up the
 * underlying `Error` and are NOT wrapped — they have no server-side
 * envelope to attach.
 */
export class IogridError extends Error {
  public readonly status: number;
  public readonly code: ErrorCode | string;
  public readonly fieldPath: string | undefined;
  public readonly metadata: Record<string, string> | undefined;
  public readonly requestId: string | undefined;

  constructor(status: number, envelope: ErrorEnvelope) {
    super(envelope.message || `iogrid: HTTP ${status}`);
    this.name = 'IogridError';
    this.status = status;
    this.code = envelope.code;
    this.fieldPath = envelope.fieldPath;
    this.metadata = envelope.metadata;
    this.requestId = envelope.requestId;
  }
}

/**
 * Helper for typed rate-limit handling. Returns the server-suggested
 * retry delay in seconds, or undefined when the response did not
 * include `Retry-After`.
 */
export function retryAfterSeconds(err: unknown): number | undefined {
  if (!(err instanceof IogridError)) return undefined;
  const v = err.metadata?.['retry_after_seconds'] ?? err.metadata?.['retryAfterSeconds'];
  if (!v) return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
}
