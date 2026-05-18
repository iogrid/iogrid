# iogridd — Rust provider daemon workspace

This is the Cargo workspace for **iogridd**, the iogrid provider-side daemon. The daemon runs on every supply-side PC / Mac and is responsible for:

- maintaining a persistent bidirectional gRPC stream to the iogrid coordinator,
- accepting and isolating workloads (bandwidth, Docker, GPU, macOS / iOS builds),
- enforcing local pre-flight anti-abuse filters that mirror the server-side rules,
- gating activity on caps + calendar + idle-detection,
- exposing a localhost HTTP+SSE bridge so the web management plane can read state and mutate config.

The architecture lives in `../docs/TECH.md`. This README is the operator surface for the codebase.

## Layout

```
daemon/
├── Cargo.toml                 [workspace manifest]
├── rust-toolchain.toml        [pinned stable 1.95]
├── .cargo/config.toml         [release lto=fat, strip, codegen-units=1]
├── Makefile                   [fmt / lint / test / build / per-target release]
└── crates/
    ├── core/                  supervisor — tokio runtime + state machine, daemon binary `iogridd`
    ├── transport/             bidi gRPC stream to coordinator (tonic, rustls mTLS)
    ├── routing/               WireGuard tunnel + SOCKS5 acceptor (boringtun + socks5-server, feature-gated)
    ├── workload-docker/       bollard-driven Docker runner (gVisor / Kata / Hyper-V isolation)
    ├── workload-gpu/          NVML on Linux/Windows, MLX/Metal on macOS (feature-gated)
    ├── workload-ios/          Tart subprocess driver — macOS only
    ├── anti-abuse/            local pre-flight filters mirroring antiabuse-svc
    ├── scheduler/             caps + calendar + idle state machine
    ├── ui-bridge/             axum HTTP+SSE on 127.0.0.1:7777 (`/state`, `/audit`, `/config`)
    ├── platform-mac/          IOKit idle, LaunchAgent paths (cfg-gated)
    ├── platform-linux/        systemd user unit paths, XScreenSaver idle (cfg-gated)
    └── platform-windows/      Windows Service paths, GetLastInputInfo idle (cfg-gated)
```

Each crate exposes only its trait + value types and a scaffold no-op / in-memory implementation. Concrete platform / vendor backends are added behind explicit Cargo features so contributor `cargo check` finishes in seconds.

## Quickstart

```bash
make fmt-check    # rustfmt --check
make lint         # clippy -D warnings (workspace-wide, all targets)
make test         # cargo test --workspace --all-targets
make build        # debug build
make build-release
```

Run the daemon binary directly from source:

```bash
cargo run -p iogrid-core --bin iogridd
```

## Target matrix

| Target triple                 | Runner OS (CI)  | Artifact       |
|-------------------------------|-----------------|----------------|
| `aarch64-apple-darwin`        | `macos-latest`  | `iogridd-aarch64-apple-darwin` |
| `x86_64-apple-darwin`         | `macos-latest`  | `iogridd-x86_64-apple-darwin`  |
| `x86_64-unknown-linux-gnu`    | `ubuntu-latest` | `iogridd-x86_64-unknown-linux-gnu` |
| `aarch64-unknown-linux-gnu`   | `ubuntu-latest` | `iogridd-aarch64-unknown-linux-gnu` (cross via `aarch64-linux-gnu-gcc`) |
| `x86_64-pc-windows-msvc`      | `windows-latest`| `iogridd-x86_64-pc-windows-msvc.exe` |

The full matrix runs in `.github/workflows/daemon-ci.yml` on every push or pull request that touches `daemon/`.

## Feature flags

| Crate                  | Feature       | What it pulls in |
|------------------------|---------------|------------------|
| `iogrid-routing`       | `routing-real`| boringtun + socks5-server (real WG + SOCKS5) |
| `iogrid-workload-docker` | `docker-real` | bollard (real Docker daemon client) |
| `iogrid-workload-gpu`  | `gpu-real`    | nvml-wrapper (Linux/Windows) or objc2+Metal (macOS) |

Default builds do NOT enable these — the scaffold compiles and passes tests on every supported target with zero vendor dependencies. CI exercises the default profile across all five targets, plus a sanity-check job that flips `routing-real` + `docker-real` on Linux.

## Adding a new crate

1. `mkdir crates/<name> && cd crates/<name>`
2. Add `Cargo.toml` (copy the boilerplate from an existing crate — the `[package]` block inherits from the workspace).
3. Add the crate path to the workspace `members` list in `daemon/Cargo.toml`.
4. Add a `[workspace.dependencies]` entry if other crates will depend on yours, then reference it as `<name> = { workspace = true }` from the consumer.
5. Write at least one `#[test]` proving the public API surface compiles. `cargo check --workspace --all-targets` must remain clean.
