# Squirrel.Windows — Windows auto-update for iogridd

Phase 2 of [EPIC #348](https://github.com/iogrid/iogrid/issues/348). Brings
[Squirrel.Windows](https://github.com/Squirrel/Squirrel.Windows) into the
Windows installer flow so providers on Windows receive new daemon releases
automatically — matching the macOS Sparkle integration shipped in Phase 1
(PR [#387](https://github.com/iogrid/iogrid/pull/387)).

## Scope (Phase 2)

- Pivot the existing WiX 4 `.msi` build to ALSO produce a Squirrel
  `iogridd-<version>-full.nupkg` per release (same Rust binary, two
  packaging formats).
- Ship `Update.exe` (the Squirrel runtime) into the install root so the
  Windows service can self-update side-by-side via `app-<version>/`
  subdirs without writing back into `Program Files\iogrid\`.
- Generate a `RELEASES` index file (Squirrel's manifest format —
  `<sha1> <pkg-filename> <bytes>` one per line) on every `v*` tag push
  and publish to `releases.iogrid.org/windows/RELEASES` via the
  GitHub-Release tarball fallback the linux track established (the
  `releases.iogrid.org` Deployment from [#393](https://github.com/iogrid/iogrid/issues/393)
  redirects to the GitHub-hosted asset).
- Authenticode-sign the .nupkg + Update.exe with the same `signtool`
  flow the .msi already uses (`WINDOWS_CERT_PFX_BASE64` +
  `WINDOWS_CERT_PASSWORD` env vars).

## Squirrel vs WiX-Burn — why Squirrel

The issue ([#389](https://github.com/iogrid/iogrid/issues/389)) listed both
options. Picked **Squirrel.Windows** for:

| Trait | Squirrel.Windows | WiX-Burn |
|---|---|---|
| Initial install | Already covered by our `iogrid.wxs` `.msi` | Replaces our installer with a `.exe` bootstrapper bundle |
| Delta updates | First-class (`.nupkg` deltas; only changed files transit) | Requires building a custom bootstrapper UI + chained MSPs |
| Manifest format | Simple `RELEASES` text file (`<sha1> <filename> <bytes>`) | XML bundle manifest, requires re-signing the entire bootstrapper |
| Used in production by | Slack, GitHub Desktop, Discord, VS Code, Atom | Visual Studio, SQL Server — heavier, MS-internal-leaning |
| Footprint on disk | `Update.exe` (~1 MB) + per-version `app-X.Y.Z\` dirs | Bootstrapper retained for repair; per-package install state |
| Operator transparency | `Update.exe --update <url>` is a single-CLI verb | `dark.exe` round-trips required to inspect the bundle |

Squirrel keeps our existing `.msi` (and the perMachine service registration
in `iogrid.wxs`) as the **initial install** vehicle while adding a
side-channel for **subsequent updates** that doesn't require UAC re-prompts
or re-running `msiexec` (which would stop+restart the service noisily).
That matches Sparkle's model on macOS, where the `.pkg` is the first hit
and the framework handles every subsequent in-place swap.

## What is OUT of scope (filed as follow-ups)

- **In-app "Check for updates…" UI.** The iogridd service is headless on
  Windows just as it is on macOS. Squirrel's `Update.exe --update` runs
  silently in the background; a tray-icon UI to manually trigger checks
  ships when a status-bar UI binary lands. Tracked by the same
  `#348-mac-statusbar-menu` follow-up the macOS track filed.
- **EV-cert acquisition for Authenticode signing.** Squirrel signs with
  Microsoft Authenticode (NOT EdDSA — see "Signing trade-off" below). A
  proper EV cert is a multi-week procurement (Sectigo / DigiCert / SSL.com).
  Until provisioned, CI produces UNSIGNED `.nupkg` artifacts for
  smoke-testing. Tracked as [#389-windows-authenticode-ev-cert](https://github.com/iogrid/iogrid/issues/) (filed alongside this PR).
- **Pipe the iogridd Rust supervisor at `daemon/crates/core/src/updater/`
  to drive Update.exe on Windows.** Today the supervisor downloads the
  binary directly via the `manifest.json` flow. On Windows we want it to
  hand off to `Update.exe --update https://releases.iogrid.org/windows`
  instead so the side-by-side `app-X.Y.Z\` convention is honoured. Filed
  as [#389-daemon-squirrel-handoff](https://github.com/iogrid/iogrid/issues/).

## Squirrel integration approach — repack the existing .msi binary

We do NOT replace the WiX `.msi`. We **repack** the same `iogridd.exe`
that `build.ps1` already produces, into a NuGet-style `.nupkg`
(Squirrel's native format), with `Update.exe` bundled alongside.

Why this approach:

1. The `.msi` already handles perMachine Windows-service registration
   (`<ServiceInstall>` in `iogrid.wxs`). Squirrel.Windows historically
   targeted per-user `%LocalAppData%` installs — its `Update.exe` runtime
   refuses to manage perMachine services. So we keep the `.msi` for the
   service-lifecycle bits and use Squirrel only for the binary swap.
2. On update, `Update.exe --update <url>` stages `app-X.Y.Z\iogridd.exe`
   next to the current `app-X.Y.W\iogridd.exe` under
   `C:\Program Files\iogrid\packages\`, then atomically updates a
   `current` symlink. The Windows service's `ImagePath` points at the
   symlink, so SCM picks up the new binary on next start.
3. The post-swap restart is driven by the Windows service control manager
   via `sc.exe stop iogridd && sc.exe start iogridd`, which is what the
   daemon supervisor would invoke today anyway.

This split — `.msi` for first install (service registration, UAC, Start
Menu shortcut), Squirrel for subsequent binary swaps — is the same split
Slack and GitHub Desktop use (Slack ships an `.msi` for enterprise IT and
the Squirrel auto-update DLL for per-user updates).

## Signing trade-off — Authenticode vs EdDSA

The macOS Sparkle integration uses **EdDSA (ed25519)** signatures over each
release artifact, with the public key embedded in `Info.plist` so each
release ships its own trust anchor.

Squirrel.Windows uses **Microsoft Authenticode** signatures because that's
what `Update.exe` and the Windows installer subsystem natively trust. Our
`build.ps1` already uses Authenticode via `signtool` for the `.msi`; we
extend the same env-var-gated signing block to the `.nupkg` + `Update.exe`.

This means:

- **Sparkle (macOS):** self-managed ed25519 key in OpenBao →
  `SPARKLE_ED_PRIVKEY_B64` GH-secret → `sign_update` in CI. NO third-party
  CA involved; trust derived from the pubkey baked into the `.app`.
- **Squirrel (Windows):** Microsoft-CA-rooted Authenticode cert (EV-cert
  preferred to avoid SmartScreen reputation cold-start) → `.pfx` projected
  as `WINDOWS_CERT_PFX_BASE64` → `signtool` in CI. Trust rooted in
  Windows' built-in CA store; user sees the iogrid Inc. publisher name
  on the UAC prompt.

The Authenticode cert is a **founder-physical action** — Sectigo / DigiCert /
SSL.com EV cert validation typically takes 1-3 weeks (notarized articles
of incorporation + DUNS-number cross-check + a Skype call). Until then
CI builds the `.nupkg` UNSIGNED and Squirrel clients reject every update
they receive — the same safest-possible-default the macOS placeholder
pubkey provides. Acquisition tracked as
[#389-windows-authenticode-ev-cert](https://github.com/iogrid/iogrid/issues/).

## File layout (in the daemon repo)

```
installer/windows/
├── build.ps1                — existing: builds the .msi via WiX
├── iogrid.wxs               — existing: WiX 4 product definition
├── app/                     — Phase 2: convention for the side-by-side
│                              install root the service launches from
│   └── README.md
├── squirrel/
│   ├── README.md            — this file
│   ├── SQUIRREL_VERSION     — pinned Squirrel.Windows release we use
│   ├── nuspec.template.xml  — NuGet spec template, envsubst-driven
│   ├── fetch-squirrel.ps1   — downloads + SHA-256-verifies the
│   │                          Squirrel runtime tarball
│   ├── build-nupkg.ps1      — packs the staged binary into iogridd-X.Y.Z-full.nupkg
│   ├── generate-releases.ps1 — produces the RELEASES manifest
│   └── pubkey.authenticode  — placeholder: CN of the (not-yet-acquired)
│                              EV cert; updated when the real cert lands
└── (releases.iogrid.org/windows/ path served via GitHub Release asset)
```

## Operator runbook — publishing a new Windows release

1. CI runs `installer-ci.yml::windows-msi`:
   - Builds `iogridd.exe` from the pinned Rust toolchain.
   - Runs `build.ps1` to produce `iogrid-X.Y.Z-x64.msi` (existing).
   - Runs `squirrel/build-nupkg.ps1` to produce
     `iogridd-X.Y.Z-full.nupkg` (new).
   - If `WINDOWS_CERT_PFX_BASE64` is set, signs both with Authenticode.
2. On `v*` tag push:
   - `squirrel/generate-releases.ps1` appends the new `.nupkg` line to
     `RELEASES`.
   - The combined tree (`RELEASES` + `iogridd-X.Y.Z-full.nupkg` +
     `iogridd-X.Y.Z-x64.msi`) is tarred and uploaded to the GitHub
     Release `windows-installers` (analogous to the `linux-repo` Release
     the Linux track established in PR
     [#395](https://github.com/iogrid/iogrid/pull/395)).
   - When `releases.iogrid.org/windows/` Deployment from
     [#393](https://github.com/iogrid/iogrid/issues/393) lands, that
     hostname redirects to the GitHub Release asset.

## Local dev — testing the .nupkg locally

```pwsh
# 1. Build the .msi + .nupkg
cd installer\windows
.\build.ps1 -DaemonExe ..\..\daemon\target\release\iogridd.exe `
            -Arch x64 -Version 0.1.0 `
            -OutDir dist
.\squirrel\build-nupkg.ps1 -DaemonExe ..\..\daemon\target\release\iogridd.exe `
                           -Version 0.1.0 `
                           -OutDir dist

# 2. Spin a local static server pretending to be releases.iogrid.org
cd dist
python -m http.server 8080

# 3. From a separate Windows host (or VM), prime the install
# (assumes the .msi has already been installed once to register the service)
"%ProgramFiles%\iogrid\Update.exe" --update http://localhost:8080/

# 4. Observe app-0.1.1\iogridd.exe staged side-by-side, current symlink
# repointed, service restarted
```

## References

- Squirrel.Windows docs: https://github.com/Squirrel/Squirrel.Windows/blob/master/docs/readme.md
- RELEASES file format: https://github.com/Squirrel/Squirrel.Windows/blob/master/docs/using/update-process.md#the-releases-file
- Authenticode signing flow: https://learn.microsoft.com/en-us/windows/win32/seccrypto/cryptography-tools#signtool
- iogrid manifest.json auto-updater (cross-platform fallback): `../../auto-update/README.md`
- Sparkle macOS counterpart: `../../macos/sparkle/README.md`
