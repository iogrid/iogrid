# syntax=docker/dockerfile:1.7
#
# Phase 0 vCard LinkedIn enrichment demo — runnable container.
#
# Multi-stage build: compile a static Go binary on golang:1.23-bookworm,
# then drop it into a distroless static base. Final image is ~12 MB,
# runs as non-root uid 65532, no shell, no package manager.
#
# Customer-side usage (as a k8s CronJob or one-shot Job):
#
#   docker run --rm \
#     -e IOGRID_API_KEY=ig_live_... \
#     -e IOGRID_WORKSPACE=vcard-prod \
#     ghcr.io/iogrid/example-vcard-enrich:latest \
#     -vanity satyanadella
#
# (Image is built by the example-vcard-ci workflow; not yet pushed —
# the push step lands in Phase 1 when we cut external customer images.)

FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY *.go ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w' \
        -o /out/vcard-enrich .

# ---- runtime ---------------------------------------------------------------
# distroless/static-debian12:nonroot — same base as the coordinator
# services. ~2 MB base + our ~10 MB binary = ~12 MB final image.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/vcard-enrich /usr/local/bin/vcard-enrich

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/vcard-enrich"]
