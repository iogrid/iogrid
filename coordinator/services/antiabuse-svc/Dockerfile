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
# service-only edits. The antiabuse-svc go.mod has a relative `replace`
# pointing at ../../internal/pb for the Connect-RPC stubs.
COPY coordinator/shared /src/coordinator/shared
COPY coordinator/internal /src/coordinator/internal
COPY coordinator/services/antiabuse-svc /src/coordinator/services/antiabuse-svc

WORKDIR /src/coordinator/services/antiabuse-svc
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' \
        -o /out/antiabuse-svc ./cmd/antiabuse-svc

# ---- runtime ----------------------------------------------------------------
# distroless/static-debian12:nonroot — no shell, no package manager, runs as
# uid 65532. Smallest practical attack surface for a static Go binary.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/antiabuse-svc /usr/local/bin/antiabuse-svc

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/antiabuse-svc"]
