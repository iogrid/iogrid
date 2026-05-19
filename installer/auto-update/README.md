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
└── (verifier lives in       daemon/crates/core/src/updater/  — Rust code)
```

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

Until then this directory is a SPEC. The Rust verifier in `daemon/crates/core/src/updater/` ships with a feature flag that defaults to OFF — daemons in Phase 0 do not actually self-update.
