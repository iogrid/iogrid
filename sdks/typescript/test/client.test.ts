import { describe, expect, it, vi } from 'vitest';
import { IogridClient, IogridError } from '../src/index.js';

/**
 * Tests use a synthetic `fetch` injected into the client. They cover the
 * HTTP surface contract — that the SDK builds correct URLs, sets the
 * right headers, handles 2xx/4xx/5xx envelopes, parses SSE streams,
 * and surfaces errors as IogridError instances.
 */

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function noContentResponse(): Response {
  return new Response(null, { status: 204 });
}

function sseResponse(events: object[]): Response {
  const body = events.map((e) => `data: ${JSON.stringify(e)}\n\n`).join('');
  return new Response(body, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

describe('IogridClient', () => {
  it('requires apiKey', () => {
    expect(() => new IogridClient({ apiKey: '' })).toThrowError(/apiKey is required/);
  });

  it('createWorkload POSTs JSON with bearer auth', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      jsonResponse(201, { id: 'w1', workspaceId: 'ws1', type: 'BANDWIDTH', status: 'queued' })
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    const w = await c.createWorkload({
      type: 'BANDWIDTH',
      bandwidth: { targetUrl: 'https://example.com' },
    });
    expect(w.id).toBe('w1');
    const [url, init] = fetchFn.mock.calls[0]!;
    expect(String(url)).toBe('https://api.iogrid.org/v1/workloads');
    expect(init.method).toBe('POST');
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer iog_test');
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body as string)).toEqual({
      type: 'BANDWIDTH',
      bandwidth: { targetUrl: 'https://example.com' },
    });
  });

  it('getWorkload encodes the path id', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        workload: { id: 'abc/def', workspaceId: 'ws', type: 'DOCKER', status: 'queued' },
      })
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    await c.getWorkload('abc/def');
    const url = String(fetchFn.mock.calls[0]![0]);
    expect(url).toBe('https://api.iogrid.org/v1/workloads/abc%2Fdef');
  });

  it('listWorkloads passes query params and skips undefined', async () => {
    const fetchFn = vi.fn().mockResolvedValue(jsonResponse(200, { workloads: [] }));
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    await c.listWorkloads({ pageSize: 50, type: 'DOCKER' });
    const url = new URL(String(fetchFn.mock.calls[0]![0]));
    expect(url.searchParams.get('pageSize')).toBe('50');
    expect(url.searchParams.get('type')).toBe('DOCKER');
    expect(url.searchParams.get('status')).toBeNull();
  });

  it('cancelWorkload DELETEs with reason query', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      jsonResponse(200, { id: 'w1', workspaceId: 'ws', type: 'BANDWIDTH', status: 'cancelled' })
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    const w = await c.cancelWorkload('w1', 'no longer needed');
    expect(w.status).toBe('cancelled');
    const url = new URL(String(fetchFn.mock.calls[0]![0]));
    expect(url.searchParams.get('reason')).toBe('no longer needed');
    expect(fetchFn.mock.calls[0]![1].method).toBe('DELETE');
  });

  it('deleteApiKey returns void on 204', async () => {
    const fetchFn = vi.fn().mockResolvedValue(noContentResponse());
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    await expect(c.deleteApiKey('key-id')).resolves.toBeUndefined();
  });

  it('listApiKeys unwraps the envelope', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      jsonResponse(200, {
        keys: [{ id: 'k1', name: 'ci', prefix: 'iog_abcd', createdAt: '2026-01-01T00:00:00Z' }],
      })
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    const keys = await c.listApiKeys();
    expect(keys).toHaveLength(1);
    expect(keys[0]?.prefix).toBe('iog_abcd');
  });

  it('throws IogridError on 4xx', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      jsonResponse(400, {
        code: 'INVALID_ARGUMENT',
        message: 'bad target',
        fieldPath: 'bandwidth.targetUrl',
        requestId: 'req-123',
      })
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    await expect(
      c.createWorkload({ type: 'BANDWIDTH', bandwidth: { targetUrl: '' } })
    ).rejects.toMatchObject({
      name: 'IogridError',
      status: 400,
      code: 'INVALID_ARGUMENT',
      fieldPath: 'bandwidth.targetUrl',
      requestId: 'req-123',
    });
  });

  it('streamWorkloadEvents iterates SSE events', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      sseResponse([
        { workloadId: 'w1', newStatus: 'queued', occurredAt: '2026-01-01T00:00:00Z' },
        { workloadId: 'w1', newStatus: 'running', occurredAt: '2026-01-01T00:00:01Z' },
        { workloadId: 'w1', newStatus: 'succeeded', occurredAt: '2026-01-01T00:00:02Z' },
      ])
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    const seen: string[] = [];
    for await (const ev of c.streamWorkloadEvents('w1')) {
      seen.push(ev.newStatus);
    }
    expect(seen).toEqual(['queued', 'running', 'succeeded']);
  });

  it('streamWorkloadEvents surfaces 4xx as IogridError', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      jsonResponse(404, { code: 'NOT_FOUND', message: 'no such workload' })
    );
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn });
    const it = c.streamWorkloadEvents('nope')[Symbol.asyncIterator]();
    await expect(it.next()).rejects.toBeInstanceOf(IogridError);
  });

  it('honours custom baseUrl', async () => {
    const fetchFn = vi.fn().mockResolvedValue(jsonResponse(200, { workloads: [] }));
    const c = new IogridClient({
      apiKey: 'iog_test',
      baseUrl: 'https://api.staging.iogrid.org/',
      fetch: fetchFn,
    });
    await c.listWorkloads();
    expect(String(fetchFn.mock.calls[0]![0])).toBe('https://api.staging.iogrid.org/v1/workloads');
  });

  it('sets User-Agent header', async () => {
    const fetchFn = vi.fn().mockResolvedValue(jsonResponse(200, { workloads: [] }));
    const c = new IogridClient({ apiKey: 'iog_test', fetch: fetchFn, userAgent: 'my-app/1.0' });
    await c.listWorkloads();
    const ua = (fetchFn.mock.calls[0]![1].headers as Record<string, string>)['User-Agent'];
    expect(ua).toContain('iogrid-sdk-typescript/');
    expect(ua).toContain('my-app/1.0');
  });
});
