FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /manifestd .

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /manifestd /manifestd
# IOGRID_MANIFEST_PATH and IOGRID_BINARY_DIR are required at runtime.
# In production they point at a ConfigMap mount (manifest.json) and an
# empty emptyDir (no on-cluster binary serving; manifest URLs redirect
# to GitHub Releases via releases.iogrid.org).
ENTRYPOINT ["/manifestd"]
