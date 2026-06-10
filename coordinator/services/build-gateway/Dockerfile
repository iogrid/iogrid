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

# Copy the shared module first so layer caching survives service-only edits.
COPY coordinator/shared /src/coordinator/shared
# build-gateway now dispatches to workloads-svc via the generated protobuf
# client (internal/workloadclient → coordinator/internal/pb), whose go.mod
# replace directive points at ../../internal/pb — so that tree must be in
# the build context, exactly as workloads-svc's Dockerfile does it.
COPY coordinator/internal /src/coordinator/internal
COPY coordinator/services/build-gateway /src/coordinator/services/build-gateway

WORKDIR /src/coordinator/services/build-gateway
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' \
        -o /out/build-gateway ./cmd/build-gateway

# ---- runtime ----------------------------------------------------------------
# distroless/static-debian12:nonroot — no shell, no package manager, runs as
# uid 65532. Smallest practical attack surface for a static Go binary.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/build-gateway /usr/local/bin/build-gateway

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/build-gateway"]
