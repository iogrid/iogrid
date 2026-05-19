# iogrid SDKs

Official customer SDKs for the [iogrid](https://iogrid.org) platform.

All four SDKs target the same customer-facing surface — defined once in
[`proto/gen/openapi/iogrid.yaml`](../proto/gen/openapi/iogrid.yaml) — and
expose the same idiomatic API in each language.

## Language matrix

| Language   | Package                                           | Path                | Transport                | Runtime requirement |
|------------|---------------------------------------------------|---------------------|--------------------------|---------------------|
| TypeScript | [`@iogrid/sdk`](typescript/README.md)             | `sdks/typescript/`  | global `fetch` (Connect-Web-compatible wire) | Node 18.17+, Bun, Deno, browsers, Cloudflare Workers |
| Python     | [`iogrid`](python/README.md) (PyPI)               | `sdks/python/`      | async `httpx`            | Python 3.10+        |
| Go         | [`github.com/iogrid/go-sdk`](go/README.md)        | `sdks/go/`          | stdlib `net/http`        | Go 1.22+            |
| Java       | [`com.iogrid:sdk`](java/README.md) (Maven Central)| `sdks/java/`        | OkHttp 4 + Jackson 2     | Java 17+            |

## Method surface

Every SDK exposes the same ten methods. Naming follows each language's idioms (camelCase / snake_case / PascalCase).

| Capability                | TypeScript / Java          | Python                       | Go                       |
|---------------------------|----------------------------|------------------------------|--------------------------|
| Submit workload           | `createWorkload`           | `create_workload`            | `CreateWorkload`         |
| Get workload              | `getWorkload`              | `get_workload`               | `GetWorkload`            |
| List workloads            | `listWorkloads`            | `list_workloads`             | `ListWorkloads`          |
| Cancel workload           | `cancelWorkload`           | `cancel_workload`            | `CancelWorkload`         |
| Stream workload events    | `streamWorkloadEvents`     | `stream_workload_events`     | `StreamWorkloadEvents`   |
| Mint API key              | `createApiKey`             | `create_api_key`             | `CreateAPIKey`           |
| List API keys             | `listApiKeys`              | `list_api_keys`              | `ListAPIKeys`            |
| Delete API key            | `deleteApiKey`             | `delete_api_key`             | `DeleteAPIKey`           |
| Get usage records         | `getUsage`                 | `get_usage`                  | `GetUsage`               |
| Get invoices              | `getInvoices`              | `get_invoices`               | `GetInvoices`            |

The TypeScript and Python SDKs return AsyncIterables for `streamWorkloadEvents`. The Go SDK returns `(<-chan WorkloadEvent, <-chan error)`. The Java SDK accepts a `Consumer<WorkloadEvent>` callback (with a `collectWorkloadEvents` helper for tests that want a buffered list).

## Wire contract

All four SDKs talk to the same HTTP+JSON surface (with HTTP/2 multiplexing where supported) defined in [`proto/gen/openapi/iogrid.yaml`](../proto/gen/openapi/iogrid.yaml). The same JSON wire format is also reachable via Connect-RPC for callers that want strongly-typed protobuf bindings (see [`proto/buf.gen.yaml`](../proto/buf.gen.yaml)).

Field names on the wire are camelCase (matches protobuf-JSON convention).
Timestamps are ISO-8601 with timezone.
Monetary amounts are `{ currency, micros }` (millionths of the major currency unit).
Every request must carry `Authorization: Bearer <api_key>`.

## Common error model

All four SDKs throw a typed `IogridError` (TypeScript / Python) / `*iogrid.Error` (Go) / `IogridException` (Java) on non-2xx responses with the same fields:

| Field         | Meaning                                                              |
|---------------|----------------------------------------------------------------------|
| `status`      | HTTP status (400-599)                                                |
| `code`        | Stable machine code mirroring `iogrid.common.v1.ErrorCode`           |
| `message`     | Human-readable English message                                       |
| `fieldPath`   | Dotted path to the offending field (e.g. `bandwidth.targetUrl`)      |
| `metadata`    | Free-form `Map<string,string>` (e.g. `retry_after_seconds`)          |
| `requestId`   | Matches `X-Request-Id` / OTel trace id for support escalation        |

## Examples

Cross-language examples live in [`../examples/`](../examples/):

* `examples/typescript-create-workload.ts`
* `examples/python-stream-events.py`
* `examples/go-bandwidth-proxy.go`
* `examples/java-api-key-rotation.java`

## Versioning

All four SDKs start at `0.1.0`. They follow semantic versioning. The
underlying protobuf contracts in [`../proto/`](../proto/) carry their
own backward-compatibility guarantees (`buf breaking` runs in CI).

## Releasing

How to cut a release for any of the four SDKs is documented in
[`RELEASING.md`](RELEASING.md). Quick reference:

```bash
make -C sdks release-ts   VERSION=0.1.0   # @iogrid/sdk → npm
make -C sdks release-py   VERSION=0.1.0   # iogrid → PyPI
make -C sdks release-go   VERSION=0.1.0   # github.com/iogrid/go-sdk
make -C sdks release-java VERSION=0.1.0   # com.iogrid:sdk → Maven Central
```

Per-SDK changelogs:

* [TypeScript](typescript/CHANGELOG.md)
* [Python](python/CHANGELOG.md)
* [Go](go/CHANGELOG.md)
* [Java](java/CHANGELOG.md)

## License

Apache-2.0 across all four SDKs.
