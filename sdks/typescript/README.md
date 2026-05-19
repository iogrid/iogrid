# @iogrid/sdk

Official TypeScript SDK for the [iogrid](https://iogrid.org) customer API.

```
npm install @iogrid/sdk
# or
pnpm add @iogrid/sdk
# or
yarn add @iogrid/sdk
# or
bun add @iogrid/sdk
```

Targets Node 18.17+, Bun, Deno, browsers, and edge runtimes. ESM-first with CJS dual-publish. TypeScript 5.x strict, ES2022 target.

## Quick start

```ts
import { IogridClient } from '@iogrid/sdk';

const iogrid = new IogridClient({ apiKey: process.env.IOGRID_API_KEY! });

const w = await iogrid.createWorkload({
  type: 'BANDWIDTH',
  bandwidth: { targetUrl: 'https://example.com/page' },
});
console.log(w.id, w.status);
```

## Examples

### 1. Submit a bandwidth proxy workload

```ts
const w = await iogrid.createWorkload({
  type: 'BANDWIDTH',
  priority: 'HIGH',
  bandwidth: {
    targetUrl: 'https://example.com/product/42',
    method: 'GET',
    preferredRegion: 'us-east-1',
    category: 'e_commerce',
  },
  labels: { campaign: 'price-watch-q4' },
});
```

### 2. Submit a Docker workload

```ts
const w = await iogrid.createWorkload({
  type: 'DOCKER',
  docker: {
    image: 'ghcr.io/example/scraper@sha256:abc...',
    command: ['./run.sh', '--target', 'amazon.com'],
    env: { CONCURRENCY: '4' },
    timeoutSeconds: 900,
    minCpuCores: 2,
    minMemoryMib: 1024,
  },
});
```

### 3. Stream a workload to completion

```ts
const w = await iogrid.createWorkload({
  type: 'DOCKER',
  docker: { image: 'ghcr.io/example/job:latest' },
});

for await (const ev of iogrid.streamWorkloadEvents(w.id)) {
  console.log(`[${ev.occurredAt}] ${ev.newStatus} — ${ev.note ?? ''}`);
}

const { result } = await iogrid.getWorkload(w.id);
console.log('done', result?.terminalStatus, 'cost:', result?.cost);
```

### 4. Mint and rotate API keys

```ts
const created = await iogrid.createApiKey({
  name: 'ci-pipeline-2026',
  scopes: ['workloads:submit'],
});
// The full secret is returned ONLY here — write it to your secret store now.
const secret = created.secret;

// List existing keys (metadata only):
for (const k of await iogrid.listApiKeys()) {
  console.log(k.id, k.name, k.prefix, k.lastUsedAt);
}

// Revoke an old key:
await iogrid.deleteApiKey('00000000-0000-0000-0000-000000000000');
```

### 5. Pull usage and invoices for the billing dashboard

```ts
const usage = await iogrid.getUsage({
  windowStart: '2026-01-01T00:00:00Z',
  windowEnd:   '2026-02-01T00:00:00Z',
  type: 'BANDWIDTH',
  pageSize: 200,
});
const totalBytes = usage.reduce((acc, r) => acc + r.quantity, 0);
console.log(`bandwidth in January: ${(totalBytes / 1e9).toFixed(2)} GB`);

const invoices = await iogrid.getInvoices();
for (const inv of invoices) {
  console.log(inv.id, inv.status, inv.total.micros / 1e6, inv.total.currency);
}
```

## Error handling

The SDK throws `IogridError` on non-2xx responses. The `code` field is the stable machine-readable error code; switch on it rather than parsing the human message.

```ts
import { IogridClient, IogridError, retryAfterSeconds } from '@iogrid/sdk';

try {
  await iogrid.createWorkload({ type: 'BANDWIDTH', bandwidth: { targetUrl: '' } });
} catch (err) {
  if (err instanceof IogridError) {
    if (err.code === 'INVALID_ARGUMENT') console.error('field:', err.fieldPath);
    else if (err.code === 'ABUSE_RATE_LIMITED') {
      const delay = retryAfterSeconds(err) ?? 5;
      console.error(`rate-limited; retry in ${delay}s`);
    } else {
      console.error('iogrid error', err.code, err.message, 'reqId:', err.requestId);
    }
  } else {
    throw err;
  }
}
```

## Cancellation and timeouts

Every method accepts an optional `AbortSignal`. The constructor's `timeoutMs` (default 30s) applies a per-request timeout. Pass `timeoutMs: 0` to disable the default for long-lived streams.

```ts
const ctl = new AbortController();
setTimeout(() => ctl.abort(), 60_000);
for await (const ev of iogrid.streamWorkloadEvents(id, ctl.signal)) {
  // ...
}
```

## Configuration

```ts
new IogridClient({
  apiKey: 'iog_…',                       // required
  baseUrl: 'https://api.iogrid.org',     // default
  timeoutMs: 30_000,                     // 0 = no timeout
  userAgent: 'my-app/1.0',               // appended to the SDK UA
  fetch: customFetch,                    // for tests / Cloudflare Workers
});
```

## License

Apache-2.0
