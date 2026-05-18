# syntax=docker/dockerfile:1.7

# ---- builder ----------------------------------------------------------------
# Multi-stage build: compile a static Go binary, then drop it into a
# distroless base. Build is cross-arch (amd64 / arm64) driven by the
# $TARGETARCH and $TARGETOS BuildKit args.
#
# The container preserves the repo layout under /src so the per-service
# go.mod's relative `replace` directives resolve against the in-tree
# shared + generated-pb modules:
#
#   replace github.com/iogrid/iogrid/coordinator/shared       => ../../shared
#   replace github.com/iogrid/iogrid/coordinator/internal/pb  => ../../internal/pb
FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# Copy the shared + generated-pb modules first so layer caching survives
# service-only edits.
COPY coordinator/shared /src/coordinator/shared
COPY coordinator/internal /src/coordinator/internal
COPY coordinator/services/proxy-gateway /src/coordinator/services/proxy-gateway

WORKDIR /src/coordinator/services/proxy-gateway
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' \
        -o /out/proxy-gateway ./cmd/proxy-gateway

# ---- runtime ----------------------------------------------------------------
# distroless/static-debian12:nonroot — no shell, no package manager, runs as
# uid 65532. Smallest practical attack surface for a static Go binary.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/proxy-gateway /usr/local/bin/proxy-gateway

USER nonroot:nonroot
# 443: customer-facing SOCKS5 / HTTP CONNECT (TLS-terminated when TLS_CERT_PATH set)
# 8080: HTTP control plane (/healthz /readyz /metrics)
EXPOSE 443 8080
ENTRYPOINT ["/usr/local/bin/proxy-gateway"]
