FROM golang:1.22-alpine AS builder

WORKDIR /build

# Copy modules
COPY coordinator/internal/pb coordinator/internal/pb
COPY coordinator/shared coordinator/shared
COPY coordinator/services/vpn-svc coordinator/services/vpn-svc

# Build
RUN cd coordinator/services/vpn-svc && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /build/vpn-svc ./cmd/vpn-svc

# Runtime
FROM alpine:3.19

RUN apk add --no-cache ca-certificates postgresql-client tini

COPY --from=builder /build/vpn-svc /bin/vpn-svc

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/bin/vpn-svc"]
