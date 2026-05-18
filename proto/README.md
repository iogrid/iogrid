# iogrid protobuf service contracts

This directory holds every service contract for the iogrid platform, managed
by [Buf](https://buf.build). The Rust daemon, the Go microservices, and the
Next.js management plane all consume code generated from these `.proto` files.

> **One contract, three languages, one source of truth.** The schema lives
> here; consumers regenerate against it.

---

## Layout

```
proto/
├── buf.yaml              # module + lint + breaking-change config
├── buf.gen.yaml          # codegen plugin pipeline (Go, TS, [Rust via tonic])
└── iogrid/
    ├── common/v1/        UUID, Money, Region, WorkloadType, ErrorCode, ErrorDetail, PageRequest/Response, TimeWindow
    ├── identity/v1/      User, Identifier, JWT claims, AuthService (Google OAuth + magic-link + step-up)
    ├── providers/v1/     Provider registration, scheduling state machine, transparency dashboard
    ├── workloads/v1/     Customer workload submission + coordinator→daemon dispatch bidi stream
    ├── antiabuse/v1/     Pre-flight filters (URL / Domain / Container Image checks) mirrored daemon-side
    └── billing/v1/       Stripe-backed subscriptions + Stripe Connect payouts
```

Every service file follows the same shape:
- `service XxxService { rpc Foo(FooRequest) returns (FooResponse); }`
- One `option go_package = "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/<area>/v1;<area>v1";` per file
- All RPCs use Connect-Go-compatible request/response messages (no streaming-only naked-value RPCs, except a handful of frame-envelope streams that are explicitly excluded from `RPC_REQUEST_STANDARD_NAME` in `buf.yaml`).

---

## Generated outputs

`buf generate` writes:

| Output | Path | Plugin |
|---|---|---|
| Go protobuf messages | `coordinator/internal/pb/iogrid/<area>/v1/*.pb.go` | `buf.build/protocolbuffers/go` |
| Connect-Go service bindings | `coordinator/internal/pb/iogrid/<area>/v1/<area>v1connect/*.connect.go` | `buf.build/connectrpc/go` |
| TypeScript protobuf messages | `web/src/lib/pb/iogrid/<area>/v1/*_pb.ts` | `buf.build/bufbuild/es` |
| Connect-Web TS service bindings | `web/src/lib/pb/iogrid/<area>/v1/*_connect.ts` | `buf.build/connectrpc/es` |
| Rust tonic bindings (planned) | `daemon/crates/transport/src/pb/` | tonic-build via cargo build script (see below) |

All generated outputs are committed to git — CI regenerates on every PR
touching `proto/` and fails if the regen produces a diff.

### Rust (tonic) bindings

The Rust daemon uses tonic's build-script flow rather than buf-managed
remote plugins, because tonic-build embeds the `.proto` parser in the Cargo
build and matches the daemon's no-network-at-build-time requirement.

`daemon/crates/transport/build.rs` will (when the Rust crate is added):

```rust
fn main() {
    tonic_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_protos(
            &[
                "../../../proto/iogrid/common/v1/types.proto",
                "../../../proto/iogrid/identity/v1/auth.proto",
                "../../../proto/iogrid/providers/v1/scheduling.proto",
                "../../../proto/iogrid/workloads/v1/dispatch.proto",
                "../../../proto/iogrid/antiabuse/v1/filters.proto",
            ],
            &["../../../proto"],
        )
        .expect("compile_protos failed");
}
```

The Rust crate scaffold lands in Track 5; for now this README documents the
intent and the proto files are already shaped to be tonic-friendly (no
buf-only extensions, no `optional` fields, no `Any`).

---

## Local workflow

```bash
# Install buf (Linux x86_64; see https://buf.build/docs/installation for others)
curl -sSL https://github.com/bufbuild/buf/releases/latest/download/buf-Linux-x86_64 \
  -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# From the repo root
make proto         # runs `buf generate` from inside proto/
make proto-lint    # `buf lint`
make proto-format  # `buf format -w` (writes formatted output in place)
make proto-check   # full CI parity: lint + format-check + generate-and-diff
```

The plugins referenced in `buf.gen.yaml` are pulled as remote plugins from
buf.build — no local `protoc-gen-*` binaries needed.

### When you change a .proto file

1. Edit the file. Add new fields with NEW field numbers; never renumber.
2. Run `make proto` to regenerate the language stubs.
3. Run `make proto-check` to ensure lint + format + breaking-change rules pass.
4. Commit the .proto AND the regenerated stubs in the same commit.

---

## Backward-compatibility rules

- `buf breaking` runs in CI with `FILE` category against `main`. Any
  field removal, type change, or renumbering that breaks wire format
  fails the build.
- Adding fields is always safe.
- Removing an enum value requires deprecating it first (mark
  `[deprecated = true]`, ship for one release, then remove in the next
  major version).
- Service RPCs may be added freely. Removing or renaming an RPC is a
  breaking change — bump the package version (`v2`) and run both side
  by side during migration.

---

## Why this layout

- **One package per bounded context** (identity / providers / workloads /
  antiabuse / billing / common) maps 1:1 to the Go microservice that owns
  the data. Cross-context reads happen via Connect-Go calls, never via
  shared protobuf types beyond `common/v1`.
- **`v1/` subdir from day one** so the migration path to `v2` is obvious
  (we keep `v1` callable, ship `v2` alongside, deprecate `v1`).
- **Connect-Go** over raw gRPC: HTTP/2 + h2c + JSON fallback gives easy
  `curl` debugging in dev, full gRPC perf in prod.
- **Common.UUID instead of `bytes`**: Postgres' native `uuid` type
  expects 36-char text, and human operators read uuids by eye in audit
  logs / Grafana panels.
- **Common.Money in micros**: floats are forbidden in any monetary path.
