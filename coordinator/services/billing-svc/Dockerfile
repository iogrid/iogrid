# syntax=docker/dockerfile:1.7

# ---- builder ----------------------------------------------------------------
# Multi-stage build: compile a static Go binary, then drop it into a
# distroless base. Build is cross-arch (amd64 / arm64) driven by the
# $TARGETARCH and $TARGETOS BuildKit args.
#
# The container preserves the repo layout under /src so the per-service
# go.mod 'replace github.com/iogrid/iogrid/coordinator/shared => ../../shared'
# resolves against the in-tree shared module.
FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# Copy the shared + generated-pb modules first so layer caching survives
# service-only edits. The billing-svc go.mod has relative `replace`
# directives pointing at ../../shared and ../../internal/pb for the
# Connect-RPC stubs (ApiKeyService, SubscriptionService, PayoutService).
COPY coordinator/shared /src/coordinator/shared
COPY coordinator/internal/pb /src/coordinator/internal/pb
COPY coordinator/services/billing-svc /src/coordinator/services/billing-svc

WORKDIR /src/coordinator/services/billing-svc
# Build BOTH entrypoints: the billing-svc API server AND the settlement-worker
# ($GRID provider-payout cron, cmd/settlement-worker). The
# settlement-worker-deployment.yaml runs the same image with the worker binary
# as its command — without this second build the worker pod CrashLoops on
# "exec: settlement-worker: no such file or directory" and no on-chain $GRID
# transfer ever happens (the grid_build_settlement rows stay tx=pending). #598.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' \
        -o /out/billing-svc ./cmd/billing-svc \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' \
        -o /out/settlement-worker ./cmd/settlement-worker

# ---- runtime ----------------------------------------------------------------
# distroless/static-debian12:nonroot — no shell, no package manager, runs as
# uid 65532. Smallest practical attack surface for a static Go binary.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/billing-svc /usr/local/bin/billing-svc
COPY --from=builder /out/settlement-worker /usr/local/bin/settlement-worker

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/billing-svc"]
