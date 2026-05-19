import { IogridError } from './errors.js';
import type { ErrorEnvelope } from './types.js';

/**
 * Minimal fetch wrapper used by the SDK. Cross-runtime: works on Node 18+
 * (global `fetch`), Bun, Deno, and the browser. Callers can substitute a
 * custom `fetch` via the client constructor for testing.
 */
export type FetchLike = (
  input: string | URL,
  init?: RequestInit
) => Promise<Response>;

export interface TransportOptions {
  baseUrl: string;
  apiKey: string;
  fetch: FetchLike;
  /** Default per-request timeout in ms. 0 = no timeout. */
  timeoutMs: number;
  /** Caller-supplied UA string suffix. */
  userAgent: string;
}

export class Transport {
  constructor(private readonly opts: TransportOptions) {}

  async request<T>(
    method: 'GET' | 'POST' | 'DELETE',
    path: string,
    body?: unknown,
    query?: Record<string, string | number | undefined>,
    signal?: AbortSignal
  ): Promise<T> {
    const url = this.buildUrl(path, query);
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.opts.apiKey}`,
      Accept: 'application/json',
      'User-Agent': this.opts.userAgent,
    };
    if (body !== undefined) headers['Content-Type'] = 'application/json';

    const ctl = new AbortController();
    const onSignal = () => ctl.abort();
    if (signal) signal.addEventListener('abort', onSignal, { once: true });
    const timer = this.opts.timeoutMs > 0
      ? setTimeout(() => ctl.abort(), this.opts.timeoutMs)
      : undefined;

    const init: RequestInit = {
      method,
      headers,
      signal: ctl.signal,
    };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
    }
    let resp: Response;
    try {
      resp = await this.opts.fetch(url, init);
    } finally {
      if (timer) clearTimeout(timer);
      if (signal) signal.removeEventListener('abort', onSignal);
    }

    if (!resp.ok) {
      let env: ErrorEnvelope;
      try {
        env = (await resp.json()) as ErrorEnvelope;
      } catch {
        env = { code: 'INTERNAL', message: `HTTP ${resp.status}` };
      }
      throw new IogridError(resp.status, env);
    }

    if (resp.status === 204) return undefined as T;
    return (await resp.json()) as T;
  }

  /**
   * Open a Server-Sent Events stream. Returns an async iterable of typed
   * event payloads. The stream completes when the server closes the
   * connection or the caller aborts via the signal.
   */
  async *stream<T>(
    path: string,
    query?: Record<string, string | number | undefined>,
    signal?: AbortSignal
  ): AsyncIterable<T> {
    const url = this.buildUrl(path, query);
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.opts.apiKey}`,
      Accept: 'text/event-stream',
      'User-Agent': this.opts.userAgent,
    };

    const init: RequestInit = { method: 'GET', headers };
    if (signal) init.signal = signal;
    const resp = await this.opts.fetch(url, init);
    if (!resp.ok) {
      let env: ErrorEnvelope;
      try {
        env = (await resp.json()) as ErrorEnvelope;
      } catch {
        env = { code: 'INTERNAL', message: `HTTP ${resp.status}` };
      }
      throw new IogridError(resp.status, env);
    }
    if (!resp.body) {
      throw new IogridError(resp.status, {
        code: 'INTERNAL',
        message: 'stream: empty body',
      });
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buffer = '';

    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        // Process complete SSE events separated by blank line.
        let sep = buffer.indexOf('\n\n');
        while (sep !== -1) {
          const raw = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);
          const data = this.parseSseEvent(raw);
          if (data !== undefined) {
            yield JSON.parse(data) as T;
          }
          sep = buffer.indexOf('\n\n');
        }
      }
    } finally {
      try { reader.releaseLock(); } catch { /* ignore */ }
    }
  }

  private parseSseEvent(raw: string): string | undefined {
    const lines = raw.split('\n');
    const dataParts: string[] = [];
    for (const line of lines) {
      // Comments per SSE spec start with ':'
      if (line.startsWith(':')) continue;
      if (line.startsWith('data:')) {
        dataParts.push(line.slice(5).trimStart());
      }
    }
    if (dataParts.length === 0) return undefined;
    return dataParts.join('\n');
  }

  private buildUrl(path: string, query?: Record<string, string | number | undefined>): string {
    const base = this.opts.baseUrl.replace(/\/+$/, '');
    const url = new URL(base + path);
    if (query) {
      for (const [k, v] of Object.entries(query)) {
        if (v === undefined || v === null || v === '') continue;
        url.searchParams.set(k, String(v));
      }
    }
    return url.toString();
  }
}
