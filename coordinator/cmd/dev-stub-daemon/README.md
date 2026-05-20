# dev-stub-daemon

Minimal Go stand-in for the production Rust `iogridd` binary. Holds open
a Dispatch bidi stream with `workloads-svc` so the rest of the
coordinator pipeline (proxy-gateway, providers-svc, vCard demo) can be
exercised end-to-end while the Rust daemon's reconnect-loop bug is
being fixed.

See:

- [iogrid#215](https://github.com/iogrid/iogrid/issues/215) — Phase 0 vCard smoke target.
- [iogrid#273](https://github.com/iogrid/iogrid/issues/273) — Rust daemon TCP-RST.

## What it does (and doesn't)

It registers as a provider, keeps the stream alive with periodic ping
frames, and answers every inbound `WorkloadAssignment` with a synthetic
`WorkloadStatusUpdate{status: FAILED}`.

On inbound `TunnelOpen` frames it dials the requested `target_host_port`
directly (egress straight from the daemon's network) and pumps bytes
both ways via `TunnelData` frames keyed by attempt_id — that is enough
to make the Phase 0 vCard smoke return a real HTTP response from
`linkedin.com` (or whichever destination the customer CONNECTed to)
through the proxy chain. Refs iogrid#279.

The stub still deliberately does NOT execute non-bandwidth workloads;
every `WorkloadAssignment` is reported `FAILED`. Tunneling is the only
real customer-job-shaped action it performs.

## Build

CI builds `dev-stub-daemon-{linux,darwin}-{amd64,arm64}` binaries on
every push to `coordinator/cmd/dev-stub-daemon/**` and uploads them as
the `dev-stub-daemon-binaries` artifact on the `coordinator-ci`
workflow.

Local build (developer-only — Phase 0 mandates ZERO local builds, use
the CI artifact):

```bash
cd coordinator/cmd/dev-stub-daemon
go build ./...
```

## Run

Requires a paired identity on disk. Run `iogridd pair <token>` once on
the host first to mint `~/.iogrid/cert.pem` + `~/.iogrid/key.pem` and
populate `provider_id` in `~/.iogrid/config.toml`.

```bash
./dev-stub-daemon-linux-amd64
```

Override knobs (all optional):

| Flag | Env | Default |
| --- | --- | --- |
| `--coordinator-url` | `IOGRID_COORDINATOR_URL` | `https://api.iogrid.org` |
| `--cert` | `IOGRID_CERT_PEM` | `~/.iogrid/cert.pem` |
| `--key` | `IOGRID_KEY_PEM` | `~/.iogrid/key.pem` |
| `--provider-id` | `IOGRID_PROVIDER_ID` | from `~/.iogrid/config.toml` |
| `--eligible-types` | `IOGRID_ELIGIBLE_TYPES` | `BANDWIDTH` |
| `--max-concurrent` | — | `4` |
| `--insecure-skip-verify` | `IOGRID_INSECURE_SKIP_VERIFY=1` | `false` |

## Phase 0 vCard smoke

1. Download the latest `dev-stub-daemon-linux-amd64` from the
   `dev-stub-daemon-binaries` artifact on the most recent green
   `coordinator-ci` run.
2. Confirm `~/.iogrid/cert.pem` + `~/.iogrid/key.pem` + a `provider_id`
   in `~/.iogrid/config.toml` exist; if not, run `iogridd pair <token>`.
3. Start the stub in one shell:
   ```bash
   ./dev-stub-daemon-linux-amd64
   ```
   Look for `stream opened, CoordinatorHello received` in the JSON log.
4. In `workloads-svc` logs, look for `daemon hello received` with the
   matching `provider_id`.
5. In a second shell, fire the vCard smoke:
   ```bash
   IOGRID_API_KEY=ig_live_phase0_... \
   IOGRID_WORKSPACE=phase0-workspace \
   PROXY_URL=proxy.iogrid.org:443 \
   ./vcard-enrich-linux-amd64 -vanity satyanadella -timeout 60s
   ```
6. Expected output: JSON with `"proxy_used": true` and a real HTTP
   response from the destination (`200`/`403`/`429` all count — the
   goal is proving the byte stream tunnels through end-to-end). A
   `malformed preamble` error or a bare `EOF` is now a regression.
