# iogrid auto-update (Sparkle-style)

The daemon polls `https://updates.iogrid.org/manifest.json` once every 24 hours (with ~10% jitter). The manifest is signed (Ed25519) with a key whose public half is **embedded in every released binary** — so a compromised CDN cannot trick the daemon into installing a malicious release.

## Flow

```
                   ┌───────────────────────────────────────────────┐
                   │ daemon (every 24h ± 10% jitter)               │
                   │                                               │
  manifest.json    │  1. GET manifest.json                         │
 ────────────────▶ │  2. verify Ed25519 sig with embedded pubkey   │
                   │  3. parse channels[].releases[]               │
                   │  4. pick highest version > self,              │
                   │     min_supported_from ≤ self                 │
                   │  5. pick artifact[] entry matching            │
                   │     <rustc target triple of self>             │
                   │  6. GET binary URL, verify SHA256             │
                   │     + (if present) cosign sig                 │
                   │  7. write to /tmp/iogridd.next                │
                   │  8. flock /usr/local/iogrid/iogridd           │
                   │  9. atomic rename + chmod +x                  │
                   │ 10. exec self via launchctl/systemd restart   │
                   └───────────────────────────────────────────────┘
```

## Why two independent signatures (Ed25519 manifest + cosign blob)

1. **Manifest signature** prevents downgrade / version-substitution attacks at the catalog layer. A compromised CDN cannot tell `0.9.0` daemons that the latest release is `0.0.1` to roll back security fixes.
2. **Cosign blob signature** prevents binary-substitution attacks on the release CDN. Even if `releases.iogrid.org` is compromised, the daemon refuses to install a binary that doesn't verify against the cosign pubkey it was shipped with.

Either signature alone is insufficient. Both must verify.

## File layout (in the daemon repo)

```
installer/auto-update/
├── README.md                — this file
├── manifest.schema.json     — JSON Schema for the manifest
├── manifest.example.json    — reference manifest you can `curl | jq` against
├── server/                  — manifestd: tiny Go server for CI + prod-edge
│   ├── main.go              — HTTP handler: /manifest.json + /<rel>/<bin>
│   ├── main_test.go         — handler unit tests
│   └── go.mod
└── (verifier lives in       daemon/crates/core/src/updater/  — Rust code)
```

The Rust verifier ships as a module of `iogrid-core`:

```
daemon/crates/core/src/updater/
├── mod.rs        — public re-exports
├── types.rs      — manifest wire types + config knobs
├── manifest.rs   — JSON parse + schema-level validation
├── verify.rs     — Ed25519 manifest sig + SHA-256 / per-binary Ed25519 sig
├── binary.rs     — atomic-replace + rollback on disk
└── worker.rs     — polling loop + Fetcher trait + UpdateHandle
```

## Operator runbook — publishing a new release

1. Build release binaries in `daemon-ci.yml` (already produces per-target
   artifacts under `iogridd-<target>`).
2. Sign each binary's hex SHA-256 with the **release-signing key**:
   ```bash
   openssl dgst -sha256 -binary iogridd-linux-amd64 | xxd -p -c 256 > /tmp/h
   echo -n "$(cat /tmp/h)" | minisign -S -s release.key -m /dev/stdin
   ```
   (Ed25519 signature, base64-encoded, goes in `artifacts[].signature`.)
3. Build the new manifest JSON. The schema is in `manifest.schema.json`.
4. Sign the manifest body with `signature.value` field cleared:
   ```bash
   jq '.signature.value = ""' manifest-new.json | tr -d '\n' \
     | openssl pkeyutl -sign -inkey release.key -rawin \
     | base64 -w0 > /tmp/sig
   jq --arg s "$(cat /tmp/sig)" '.signature.value = $s' manifest-new.json > manifest-signed.json
   ```
5. Upload signed manifest to `s3://updates.iogrid.org/manifest.json` (or the
   manifestd's `IOGRID_MANIFEST_PATH`).
6. Upload binaries under `<release-version>/<artifact-name>` matching the URLs
   embedded in the manifest.
7. CDN (Cloudflare) purges within 30 s; daemons pick up on their next 6-hour
   poll, or immediately when the operator clicks **Check now** in
   `/account/updates`.

## Test the flow end-to-end (CI integration)

The `installer-ci.yml` workflow's `auto-update-server` job builds and smokes
the `manifestd` binary. Locally:

```bash
cd installer/auto-update/server
go run . &
export IOGRID_MANIFEST_PATH=$PWD/../manifest.example.json
export IOGRID_BINARY_DIR=$PWD/test-binaries
curl http://localhost:8088/manifest.json
```

To drive the Rust daemon against it set:
```toml
# ~/.iogrid/config.toml
[updater]
manifest_url      = "http://localhost:8088/manifest.json"
channel           = "stable"
disabled          = false
poll_interval_secs = 30
```

Then `iogridd update --check` runs a single poll iteration and prints the
outcome JSON.

## Public key rotation

Each release embeds a small set of trusted update-signing pubkeys (currently 2: one current + one future). When the current key is retired, a release with a new `iogrid-update-YYYY-N` key id ships first; once the previous-current key is gone from the embedded set we can rotate the actual signing.

This is the same scheme Tor uses for directory authorities and apt uses for repository signing keys.

## Atomic replacement

On Unix: write next binary to `<install>/iogridd.next`, then `rename(2)` over `<install>/iogridd`. `rename(2)` is atomic on the same filesystem. The currently-running process keeps its file mapping (Unix doesn't enforce write-locks on running binaries), and the next invocation gets the new binary. We then send `SIGTERM` to ourselves and `launchctl`/`systemd` restarts us with the new image.

On Windows: identical scheme via `MoveFileExW(MOVEFILE_REPLACE_EXISTING)`. The service manager handles the restart.

## Rollback safety

Before swapping, we keep `<install>/iogridd.prev`. If the new binary exits with code 78 (`EX_CONFIG`) or fails its first health-check within 30s, the daemon's pre-exec wrapper (a tiny shim provided by `iogrid-platform-{mac,linux,windows}`) restores the `.prev` copy and reports the failure to the coordinator.

## CI / release wiring (TODO when we cut 0.1.0)

A release tag (`v0.1.0`) triggers:
1. installer-ci.yml builds .pkg/.msi/.deb/.rpm
2. release-publish.yml uploads to releases.iogrid.org and updates manifest.json with the new release stanza + Ed25519 sig
3. Flux updates `iogrid/iogrid-ops` repo to bump the served manifest

## Phase 1 → 2 transition

* **Phase 0 (PR #139)** — schema + signing scaffolding shipped. Daemons did
  not poll.
* **Phase 1 (this PR, issue #59)** — polling worker + atomic-replace +
  Ed25519 verifier + web UI active. Defaults to `disabled = true` so existing
  Phase-0 installations don't auto-update without operator opt-in.
* **Phase 2** — operator UI flipped on by default at install time once the
  release-signing HSM is provisioned and the first signed manifest is
  published at `updates.iogrid.org`.

The Rust verifier's trust root is currently a placeholder (32 zero bytes) so
production-style auto-updates fail closed until the build sets
`IOGRID_TRUSTED_PUBKEYS` at compile time. The CLI's `iogridd update --check`
prints the verification error verbatim, which is how operators discover this
state during dogfooding.
