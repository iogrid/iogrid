# Sparkle 2.x — macOS auto-update for iogridd

Phase 1 of [EPIC #348](https://github.com/iogrid/iogrid/issues/348). Brings the
[Sparkle](https://sparkle-project.org) framework into the macOS `.pkg` so
providers on macOS receive new daemon releases automatically.

## Scope (Phase 1)

- Embed `Sparkle.framework` (2.6.x binary release) into `iogridd.app/Contents/Frameworks`.
- Add `SUFeedURL` + `SUPublicEDKey` + automatic-check keys to `iogridd.app/Contents/Info.plist`.
- Sign the appcast feed with an ed25519 keypair; private half in the K8s
  Secret `iogrid-sparkle-signing`, public half checked into `pubkey.ed25519`.
- CI step (`daemon-ci.yml::generate-appcast`) regenerates the appcast.xml on
  every `v*` tag push and uploads it to the static path served at
  `https://releases.iogrid.org/macos/appcast.xml`.

## What is OUT of scope (filed as follow-ups)

- The in-app **"Check for updates…"** menu item. Today's daemon is a headless
  LaunchAgent — there's no status-bar UI to attach a menu to. Sparkle still
  runs in the background (the framework supports headless `SPUUpdater`); the
  menu item is purely a manual-trigger affordance and ships when we add a
  status-bar UI binary. Tracked as `#348-mac-statusbar-menu`.
- Windows MSI auto-update (`#348-windows-msi-auto-update`).
- Linux .deb/.rpm/.apk repos (`#348-linux-deb-rpm-apk-repos`).

## Sparkle integration approach — binary framework

We use the **binary framework distribution** from the Sparkle GitHub
[releases](https://github.com/sparkle-project/Sparkle/releases), not the
Swift Package Manager target. Reasoning:

1. iogridd is a Rust binary (`cargo build --release`). It's NOT an Xcode
   project, so SwiftPM gives us nothing — the compiler-level SwiftPM integration
   only works for Swift consumers. Rust would still need to dlopen the framework.
2. The `.app` bundle structure expected by macOS Gatekeeper is identical either
   way; Sparkle's installer XPC services live in `Contents/Frameworks/Sparkle.framework/Versions/B/Resources/`
   and are wired by Sparkle's `Info.plist`, NOT by the Rust binary.
3. The Rust binary doesn't link Sparkle. The framework runs entirely
   side-by-side: when the `iogridd.app` bundle is launched (which the
   LaunchAgent does NOT do today — `iogridd` is exec'd directly from
   `/usr/local/iogrid/iogridd`), `SPUStandardUpdaterController` initialises
   from the Info.plist keys and starts its own check loop. For headless
   daemon installs Sparkle 2 supports the `--background` install mode via
   the Installer XPC service, no UI required.

`fetch-sparkle.sh` downloads, verifies the tarball SHA-256, and places the
framework into the .app bundle. CI runs this on every macOS-pkg build.

## ⚠ Placeholder pubkey shipped in Phase 1

The `pubkey.ed25519` file currently committed in this directory is **NOT a real
signing key** — it's a deterministic SHA-256 hash, base64-encoded, that
satisfies Sparkle's plist parser (44 base64 chars representing 32 bytes) but
has NO matching private key in existence. This is deliberate: it lets the
.pkg build succeed in CI before the real keypair is provisioned in OpenBao.

Before the first real release tag (`v0.2.0+`):

1. The platform team runs `generate-keypair.sh /tmp/iogrid-sparkle-prod` on
   an air-gapped machine.
2. Commits the real `pubkey.ed25519` via a follow-up PR titled
   `feat(installer/macos): land real Sparkle signing pubkey`.
3. Stores the private key in OpenBao at `kv/iogrid/sparkle/privkey.ed25519`.
4. Sets the GitHub Actions repository secret `SPARKLE_ED_PRIVKEY_B64` to
   `base64 <<< <contents-of-privkey.ed25519>`.
5. Pushes a `v0.2.0` tag and verifies the appcast.xml validates against the
   committed pubkey via `sign_update -p <pubkey> <pkg>`.

Until step 1-4 complete, Sparkle clients will reject every update they
receive — which is the safest possible state for an empty install base.

## EdDSA signing

Sparkle 2 uses **EdDSA (ed25519)** signatures over each release artifact. The
`sign_update` tool from the Sparkle dist computes the signature; we run it in
CI from the private key materialised by external-secrets from
`iogrid-sparkle-signing`.

To rotate the signing key:

1. Bump the keypair locally: `./installer/macos/sparkle/generate-keypair.sh`
2. Update `pubkey.ed25519` in this directory + the `SUPublicEDKey`
   placeholder in `installer/macos/app/Contents/Info.plist`.
3. Update the Secret `iogrid-sparkle-signing` (the template at
   `infra/k8s/base/sparkle/secret.yaml` shows the keys; the actual values
   live in OpenBao + are projected by external-secrets).
4. Ship a `feat(installer/macos): rotate sparkle signing key` PR. Old
   installs continue to trust the old key UNTIL they pick up the new .app
   in a future release — so don't revoke the old key until 100% rollout
   confirmed in `releases-svc` telemetry.

## Local dev — testing the appcast locally

```bash
# 1. Build the .pkg with a dev keypair
./installer/macos/sparkle/generate-keypair.sh /tmp/iogrid-sparkle-dev
make -C installer/macos all \
    VERSION=0.1.0 ARCH=arm64 \
    DAEMON_BIN=../../daemon/target/aarch64-apple-darwin/release/iogridd \
    SU_PUBLIC_ED_KEY="$(cat /tmp/iogrid-sparkle-dev/pubkey.ed25519)"

# 2. Serve the .pkg + appcast.xml locally
./installer/macos/sparkle/dev-serve.sh /tmp/iogrid-sparkle-dev

# 3. Install the .pkg, then point Sparkle at the local feed
defaults write org.iogrid.daemon SUFeedURL http://localhost:8080/appcast.xml
launchctl kickstart -k gui/$(id -u)/org.iogrid.daemon
```

## References

- Sparkle docs: https://sparkle-project.org/documentation/
- EdDSA signing flow: https://sparkle-project.org/documentation/eddsa-signing-of-updates/
- Headless updates (no UI): https://sparkle-project.org/documentation/headless-updates/
- Existing iogrid manifest.json auto-updater (Linux/Windows): `../auto-update/README.md`
