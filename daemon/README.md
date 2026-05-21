# iogridd — Rust provider daemon workspace

This is the Cargo workspace for **iogridd**, the iogrid provider-side daemon. The daemon runs on every supply-side PC / Mac and is responsible for:

- maintaining a persistent bidirectional gRPC stream to the iogrid coordinator,
- accepting and isolating workloads (bandwidth, Docker, GPU, macOS / iOS builds),
- enforcing local pre-flight anti-abuse filters that mirror the server-side rules,
- gating activity on caps + calendar + idle-detection,
- exposing a localhost HTTP+SSE bridge so the web management plane can read state and mutate config.

The architecture lives in `../docs/ARCHITECTURE.md`. This README is the operator surface for the codebase.

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
| `iogrid-workload-docker` | `integration-docker` | bollard + the live-host integration test under `tests/integration_docker.rs`. Requires a reachable Docker daemon. |
| `iogrid-workload-gpu`  | `gpu-real`    | bollard with NVIDIA Container Toolkit runtime (Linux/Windows). Refer to `iogrid_workload_gpu::NvidiaContainerRunner`. |
| `iogrid-workload-gpu`  | `gpu-mlx`     | Apple-Silicon MLX runner (`MlxRunner`). Pulls a pre-built `ghcr.io/iogrid/mlx-runtime` image via Docker Desktop, passes the customer `MlxSpec` (`model_name` / `vram_gb_required` / `batch_size` / `prompt`) as env vars, captures inference stdout. Requires macOS 14+ on `aarch64-apple-darwin`; off-target builds keep the stub. |
| `iogrid-workload-gpu`  | `integration-mlx` | Live-host MLX integration test (off by default; flips on `gpu-mlx`). Only meaningful on a self-hosted Apple-Silicon runner with Docker Desktop + the `mlx-runtime` image pre-pulled. |

Default builds do NOT enable these — the scaffold compiles and passes tests on every supported target with zero vendor dependencies. CI exercises the default profile across all five targets, plus a sanity-check job that flips `routing-real` + `docker-real` on Linux.

## Workload runners

The daemon ships three workload-execution engines (PRs #12, #13, #14):

* **`iogrid-workload-docker`** — bollard-backed Docker runner. Pulls only from images on the anti-abuse registry allowlist (`docker.io`, `ghcr.io`, `registry.iogrid.org`, `public.ecr.aws` by default), creates a container with cgroup CPU/memory caps, read-only rootfs, `no-new-privileges`, all caps dropped, attached to the iogrid-managed `iogrid-egress` bridge so customer outbound traffic flows through the same SOCKS5 / anti-abuse pipeline as the bandwidth workload. Wall-clock time-boxed by `timeout_secs`; logs captured + capped at 1 MiB; container + image cleanup on exit.
  * Requirement: **Docker Engine / Docker Desktop** on the provider machine. Mac users: install Docker Desktop and ensure the `docker` CLI is on `$PATH` (see issue #81). Linux: any recent Docker Engine works; for hardened isolation pin the container runtime to `runsc` (gVisor) or `kata`.
  * To use the live bollard runtime, build the daemon with `--features docker-real` (pulls bollard into the workspace) and call `BollardDockerRunner::connect_local`.
* **`iogrid-workload-gpu`** — GPU container runner. Two backends, picked by `cfg`:
  * Linux/Windows with `gpu-real`: bollard + `host_config.runtime = "nvidia"` (NVIDIA Container Toolkit). Requires the NVIDIA driver + container toolkit pre-installed on the host.
  * macOS aarch64 with `gpu-mlx`: real `MlxRunner` (issue #13). Strategy is to run a pre-built `ghcr.io/iogrid/mlx-runtime` container via Docker Desktop — the image ships Apple's MLX wheels (`mlx`, `mlx-lm`, `mlx-vlm`); the runner passes the customer assignment (`MlxSpec { model_name, vram_gb_required, batch_size, prompt }`) as `MLX_MODEL` / `MLX_VRAM_GB` / `MLX_BATCH_SIZE` / `MLX_PROMPT` env vars, waits for the container to exit, and streams inference stdout (capped at 1 MiB) back to the coordinator. The container has no host networking, `readonly_rootfs`, `no-new-privileges`, all caps dropped — same anti-abuse posture as the CUDA path. A lower-level objc2 → MLX C API binding is the planned faster follow-up.
    * **Requirement: macOS 14 (Sonoma) or newer + Apple Silicon.** Apple's MLX framework is Sonoma+. `MlxRunner::connect_local` runs `sw_vers -productVersion` at startup and returns `GpuError::HostTooOld` on older versions; the supervisor must NOT advertise `GPU_MLX` workloads on older hosts.
    * **Requirement: Docker Desktop** running locally (the runner reuses bollard's default unix-socket transport).
    * Off-target builds (Linux, Windows, x86_64-apple-darwin) compile the `MlxStubGpuRunner` instead — every assignment returns `BackendUnimplemented`. The same stub is selected on `aarch64-apple-darwin` when the crate is built without `--features gpu-mlx`.
  * The supervisor must therefore filter eligible workload types it advertises based on the runner's `backend()` slug (`"nvidia"`, `"mlx"`, `"mlx-stub"`, `"noop"`).
* **`iogrid-workload-ios`** — Tart subprocess driver. Spawns macOS VMs via the `tart` CLI (clone → set CPU/memory → run --no-graphics → poll `tart ip` → ssh `sshpass` → scp artifact out → curl PUT to coordinator-supplied URL → tart delete). Mac-only — every other target's `TartRunner::run` returns `IosBuildError::UnsupportedPlatform`.
  * Requirement: **macOS 15 Sequoia** or newer on the provider machine. The supervisor uses `iogrid_platform_mac::supports_ios_build()` at startup; on older macOS it must NOT advertise `IOS_BUILD` as an eligible workload type. Build hosts also need `tart`, `sshpass`, and `curl` on `$PATH` (all `brew install`-able).
  * Recommended base image: `ghcr.io/cirruslabs/macos-sequoia-xcode:latest`. The Tart default VM password is `admin` (already wired into the runner's defaults).

The supervisor (`iogrid_core::WorkloadRouter`) decodes `DispatchFrame::NewAssignment` envelopes and routes them to the right runner. It tracks active assignments in `ActiveRegistry`, surfaces `running` → `succeeded` / `failed` / `timed_out` / `cancelled` `Update` frames back to the coordinator, and revokes every in-flight runner when the scheduler flips to `Paused` or when the daemon shuts down.

## Auto-update (issue #59)

The daemon polls a signed manifest at `https://updates.iogrid.org/manifest.json`
(override via `[updater].manifest_url`), picks the highest-semver release on
its configured channel, verifies the manifest's Ed25519 signature against an
embedded trust root, downloads + SHA-256-verifies the per-target binary,
stages it at `<install-dir>/iogridd.new`, then on operator-confirmed restart
atomic-renames it over `iogridd` while keeping the previous version at
`iogridd.old` for one cycle.

CLI:

```bash
iogridd update --check     # poll once, print JSON outcome
iogridd update --apply     # rename .new over current binary
iogridd update --rollback  # restore .old binary
```

Config (in `~/.iogrid/config.toml`):

```toml
[updater]
manifest_url       = "https://updates.iogrid.org/manifest.json"
channel            = "stable"   # or beta / edge
disabled           = true       # default: opt-in
poll_interval_secs = 21600      # 6h, override for tests
```

Module layout:

```
daemon/crates/core/src/updater/
├── mod.rs        — public re-exports
├── types.rs      — UpdateConfig + manifest wire types
├── manifest.rs   — JSON parse + schema validation
├── verify.rs     — Ed25519 manifest sig + SHA-256 / per-binary Ed25519 sig
├── binary.rs     — atomic-replace + rollback on disk
└── worker.rs     — polling loop + Fetcher trait + UpdateHandle
```

Rollback safety: if the new binary crashes within 30s of first launch, the
pre-exec wrapper (in `iogrid-platform-{mac,linux,windows}`) restores
`iogridd.old`. See `installer/auto-update/README.md` for the operator
runbook and CI test harness.

## Adding a new crate

1. `mkdir crates/<name> && cd crates/<name>`
2. Add `Cargo.toml` (copy the boilerplate from an existing crate — the `[package]` block inherits from the workspace).
3. Add the crate path to the workspace `members` list in `daemon/Cargo.toml`.
4. Add a `[workspace.dependencies]` entry if other crates will depend on yours, then reference it as `<name> = { workspace = true }` from the consumer.
5. Write at least one `#[test]` proving the public API surface compiles. `cargo check --workspace --all-targets` must remain clean.
