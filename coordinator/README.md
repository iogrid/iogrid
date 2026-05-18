# iogrid coordinator

Server-side Go microservices that run the iogrid mesh control plane. Each
service is a separate Go module sharing a single `go.work` workspace and a
common library of bootstrap helpers under `coordinator/shared/`.

## Layout

```
coordinator/
├── go.work                              # ties every module together
├── shared/                              # shared bootstrap library
│   ├── health/        — /healthz + /readyz registry
│   ├── log/           — slog/JSON setup
│   ├── otel/          — OpenTelemetry SDK init (OTLP/gRPC)
│   ├── db/            — pgx pool + goose migration runner
│   └── server/        — chi router + otelhttp + Prometheus + graceful shutdown
├── services/                            # one Go module per bounded context
│   ├── identity-svc/        Google OAuth, magic-link, JWT issuance
│   ├── providers-svc/       Provider registry, scheduling state, transparency feed
│   ├── workloads-svc/       Customer workload submission + dispatch + retry
│   ├── antiabuse-svc/       Pre-flight filtering + abuse detection
│   ├── billing-svc/         Stripe subscriptions + Connect payouts + metering
│   ├── telemetry-svc/       Metric / log / trace ingestion + alerting
│   ├── gateway-bff/         BFF for the Next.js management plane
│   ├── proxy-gateway/       SOCKS5 / HTTP-CONNECT customer entrypoint
│   └── build-gateway/       iOS-CI scheduling entrypoint (Mac providers + S3)
└── charts/iogrid/                       # Helm chart for all 9 services
    ├── Chart.yaml
    ├── values.yaml                      # one services.<name> block per microservice
    └── templates/
        ├── _helpers.tpl
        ├── serviceaccount.yaml
        ├── deployment.yaml
        ├── service.yaml
        ├── hpa.yaml                     # opt-in per service via autoscaling.enabled
        └── networkpolicy.yaml           # intra-mesh + ingress only
```

## Per-service shape

Every microservice follows the same skeleton:

```
services/<svc>/
├── go.mod                          # uses replace -> ../../shared
├── Dockerfile                      # multi-stage, distroless final, CGO_ENABLED=0
├── cmd/<svc>/main.go               # entrypoint: log/otel/server wiring
└── internal/server/routes.go       # service-specific HTTP routes
```

`main.go` boots:

1. `log.Setup(serviceName)` — slog JSON logger to stdout.
2. `otel.Setup(ctx, ...)` — OpenTelemetry SDK with OTLP/gRPC exporter (no-op
   when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset).
3. `health.New().MarkReady()` — readiness latch.
4. `sharedserver.Run(...)` — chi router with `/healthz`, `/readyz`,
   `/metrics`, plus the service's own routes; `otelhttp` wrapping; graceful
   shutdown on `SIGINT` / `SIGTERM`.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `LISTEN_ADDR` | `:8080` | HTTP bind address |
| `LOG_LEVEL` | `info` | slog level: `debug` / `info` / `warn` / `error` |
| `DEPLOY_ENV` | `dev` | populated into the OTel resource as `deployment.environment` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | when set, traces are exported via OTLP/gRPC |
| `DATABASE_URL` | unset | libpq-style connection string consumed by `shared/db` |

## Building locally

Each module is self-contained — pick one and run from inside it:

```
cd coordinator/services/identity-svc
go build ./...
go vet ./...
go test ./...
```

Cross-arch container build (matches CI):

```
docker buildx build \
  -f coordinator/services/identity-svc/Dockerfile \
  --platform linux/amd64,linux/arm64 \
  --tag ghcr.io/iogrid/identity-svc:dev \
  .
```

## Deploying with Helm

```
helm install iogrid coordinator/charts/iogrid \
  --namespace iogrid --create-namespace \
  --set imageRegistry=ghcr.io/iogrid \
  --set services.identity-svc.image.tag=<sha>
```

The default chart renders 9 Deployments, 9 Services, 9 ServiceAccounts, 1
HPA (proxy-gateway has `autoscaling.enabled=true` by default), and 9
NetworkPolicies. Other services opt into HPA via
`services.<name>.autoscaling.enabled=true`.

## CI

`.github/workflows/coordinator-ci.yml` runs on any push touching
`coordinator/**`:

1. **go-quality** — golangci-lint + per-module `go vet`, `go test`,
   `go build`.
2. **docker** — matrix build (9 services × 2 archs = 18 image variants),
   pushed to `ghcr.io/iogrid/<svc>` with SHA + `latest` (on `main`) tags.
