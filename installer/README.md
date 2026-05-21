# iogrid installers

End-state: a non-technical user clicks one link on **iogrid.org/install**, gets the correct double-clickable installer for their OS, clicks Continue 3 times, sees a browser tab open with a sign-in form, signs in with Google, and their PC is contributing. Total time: ~2 minutes (mostly Docker Desktop download on first-time installs).

Power users (developers, sysadmins) get the same end state from `curl | sh`.

---

## What's in here

```
installer/
├── install.sh                   # curl-pipe-sh for Mac + Linux
├── install.ps1                  # iwr|iex for Windows
├── macos/                       # .pkg recipe (pkgbuild + productbuild)
│   ├── Makefile
│   ├── distribution.xml
│   ├── Resources/{Welcome,License,Conclusion}.html
│   ├── scripts/postinstall      # runs as root after extract
│   └── templates/org.iogrid.daemon.plist.tmpl
├── windows/                     # .msi recipe (WiX 4)
│   ├── iogrid.wxs
│   └── build.ps1
├── linux/                       # .deb / .rpm / .apk via nfpm
│   ├── nfpm.yaml
│   ├── systemd/iogridd.service
│   ├── systemd/iogridd-user.service.template
│   ├── config/config.toml
│   ├── scripts/{preinstall,postinstall,preremove,postremove}.sh
│   └── build.sh
├── auto-update/                 # Sparkle-style updater spec
│   ├── README.md
│   ├── manifest.schema.json
│   └── manifest.example.json
├── common/                      # shared scaffolds
└── README.md                    # this file
```

---

## Platform support matrix

| Platform                     | curl-pipe install   | Double-click installer    | Signed?         | Auto-update |
|------------------------------|---------------------|---------------------------|-----------------|-------------|
| macOS 13+ (Apple Silicon)    | `install.sh`        | `iogrid-0.1.0-arm64.pkg`  | Developer ID + notarised (gated on `APPLE_DEVELOPER_ID`) | yes |
| macOS 13+ (Intel)            | `install.sh`        | `iogrid-0.1.0-amd64.pkg`  | Developer ID + notarised (gated on `APPLE_DEVELOPER_ID`) | yes |
| Windows 10/11 (x64)          | `install.ps1`       | `iogrid-0.1.0-x64.msi`    | EV cert (gated on `WINDOWS_CERT_PFX_BASE64`)             | yes |
| Windows 11 (ARM64)           | `install.ps1`       | `iogrid-0.1.0-arm64.msi`  | EV cert (gated on `WINDOWS_CERT_PFX_BASE64`)             | yes |
| Ubuntu / Debian (amd64)      | `install.sh`        | `iogrid_0.1.0_amd64.deb`  | cosign (gated on `COSIGN_KEY_FILE`)                       | yes |
| Ubuntu / Debian (arm64)      | `install.sh`        | `iogrid_0.1.0_arm64.deb`  | cosign (gated on `COSIGN_KEY_FILE`)                       | yes |
| Fedora / RHEL / Rocky        | `install.sh`        | `iogrid-0.1.0-1.x86_64.rpm` | GPG (`RPM_SIGNING_KEY_FILE`) + cosign                   | yes |
| Alpine Linux                 | `install.sh`        | `iogrid_0.1.0_x86_64.apk` | abuild (`APK_SIGNING_KEY_FILE`) + cosign                  | yes |
| Arch / Manjaro               | `install.sh`        | `iogrid-0.1.0-x86_64.pkg.tar.zst` (TODO: makepkg)  | (TODO) | yes |

Signing is **optional in CI** — if the secret isn't present (forks, PRs) we still build unsigned artifacts so contributors can smoke-test. Signing only runs on the official release runner where the secrets live.

---

## End-to-end install (curl-pipe path)

### Mac

```bash
curl -fsSL https://iogrid.org/install/mac | sh
```

Steps:
1. Detects `Darwin` + `arm64`/`amd64`.
2. If Docker Desktop is missing: downloads the signed `Docker.dmg`, mounts it, copies `Docker.app` to `/Applications`, launches it (the user clicks once to accept Docker's license).
3. Downloads `iogridd-darwin-{arm64,amd64}` from `releases.iogrid.org`, verifies SHA256.
4. Drops the binary at `/usr/local/iogrid/iogridd`. Symlinks `/usr/local/bin/iogridd`.
5. Writes `~/Library/LaunchAgents/org.iogrid.daemon.plist`. Calls `launchctl bootstrap` + `enable` + `kickstart` to start it.
6. Calls `iogridd pair --request` → daemon prints a 6-character code.
7. Calls `open https://app.iogrid.org/onboard/<code>` to start the browser flow.

### Linux

```bash
curl -fsSL https://iogrid.org/install/linux | sudo sh
```

Steps:
1. Detects distro from `/etc/os-release` (apt / dnf / pacman / apk).
2. Installs Docker via the distro's signed channel.
3. Drops the binary at `/usr/local/bin/iogridd`.
4. Writes `~/.config/systemd/user/iogridd.service` (preferred user-mode unit) — falls back to a system unit when run as actual root.
5. `loginctl enable-linger $USER` + `systemctl --user enable --now iogridd.service`.
6. If `$DISPLAY` or `$WAYLAND_DISPLAY` is set: `xdg-open` to the onboarding URL. Otherwise prints the URL.

### Windows

```powershell
iwr -useb https://iogrid.org/install/win | iex
```

Steps:
1. Self-elevates via `Start-Process -Verb RunAs`.
2. Detects x64 vs ARM64.
3. If Docker Desktop is missing: silent install via `Docker Desktop Installer.exe install --quiet --accept-license`.
4. Drops `iogridd.exe` at `C:\Program Files\iogrid\`.
5. `sc.exe create iogridd binPath= ... start= auto`. Recovery: 3 restarts on failure.
6. `Start-Service iogridd` → daemon mints code → `Start-Process` opens browser at `https://app.iogrid.org/onboard/<code>`.

---

## End-to-end install (double-click path)

### macOS .pkg

Apple's standard productbuild workflow. The .pkg:
* Lays down `/usr/local/iogrid/iogridd`
* Drops `/Library/LaunchAgents/org.iogrid.daemon.plist` (template, root-owned)
* Runs `scripts/postinstall` as root which:
  1. Re-templates the plist into the **installing user's** `~/Library/LaunchAgents/` (user-mode unit — daemon never runs as root per `docs/ARCHITECTURE.md`)
  2. Deletes the root-owned copy
  3. `launchctl bootstrap gui/<uid>` + `enable` + `kickstart`
  4. Calls `iogridd pair --request` and writes the code to `~/.iogrid/pairing-code.txt`
  5. Opens the browser to `app.iogrid.org/onboard/<code>` (as the user, not root)

Build it locally:

```bash
cd installer/macos
make all VERSION=0.1.0 ARCH=arm64 \
    DAEMON_BIN=../../daemon/target/aarch64-apple-darwin/release/iogridd
# Output: dist/iogrid-0.1.0-arm64.pkg
```

To sign + notarise, set:

```bash
export APPLE_DEVELOPER_ID="Developer ID Installer: iogrid (TEAMID)"
export APPLE_NOTARY_PROFILE="iogrid-notary"   # keychain profile
make all VERSION=0.1.0 ARCH=arm64 DAEMON_BIN=...
```

The notarisation step uses `xcrun notarytool submit --wait --keychain-profile $APPLE_NOTARY_PROFILE`, then `stapler staple`.

### Windows .msi

WiX 4 source at `installer/windows/iogrid.wxs`. Builds an MSI that:
* Installs `iogridd.exe` to `C:\Program Files\iogrid\`
* Registers a Windows service `iogridd` with auto-start + restart-on-failure
* Adds a "Finish iogrid setup" Start Menu shortcut to the onboarding URL
* Runs a custom action on first install that opens the browser to the onboarding URL

Build:

```powershell
./installer/windows/build.ps1 `
    -DaemonExe daemon/target/x86_64-pc-windows-msvc/release/iogridd.exe `
    -Arch x64 `
    -Version 0.1.0
# Output: dist/iogrid-0.1.0-x64.msi
```

Signing: set `WINDOWS_CERT_PFX_BASE64` (base64-encoded PFX) and `WINDOWS_CERT_PASSWORD` env vars. `build.ps1` invokes `signtool sign /tr http://timestamp.digicert.com /td SHA256 /fd SHA256`.

### Linux .deb / .rpm / .apk

[nfpm](https://nfpm.goreleaser.com/) reads `installer/linux/nfpm.yaml` and emits all three formats from the same payload definition.

```bash
cd installer/linux
IOGRID_VERSION=0.1.0 \
IOGRID_ARCH=amd64 \
IOGRID_BIN=../../daemon/target/x86_64-unknown-linux-gnu/release/iogridd \
./build.sh
# Output: ../dist/iogrid_0.1.0_amd64.deb
#         ../dist/iogrid-0.1.0-1.x86_64.rpm
#         ../dist/iogrid_0.1.0_x86_64.apk
```

The `.deb`/`.rpm`/`.apk` only register the **system-mode** systemd unit (`/lib/systemd/system/iogridd.service`) because at package-install time we don't know who the human user is. The curl-pipe `install.sh` registers the preferred user-mode unit; the package install is for server / power-user installs where system-mode is what you want anyway.

Cosign blob signing: set `COSIGN_KEY_FILE=./cosign.key` (output of `cosign generate-key-pair`). The auto-updater verifies each binary against the embedded public half before swapping.

---

## Onboarding handshake (after install)

Once the daemon is running, the browser tab opens at `app.iogrid.org/onboard/<code>`:

```
┌──────────────────────────────────────────────────┐
│ Pairing code: ABC123                             │
│                                                  │
│ Welcome to iogrid                                │
│ Three quick choices and your machine will start  │
│ earning.                                         │
│                                                  │
│ [1] Resources                                    │
│   Bandwidth: 50 GB / month  (slider)             │
│   CPU:       30 %           (slider)             │
│   ☑ Only run when I'm away                       │
│                                                  │
│ [2] Categories — what kinds of traffic?          │
│ [3] Payout — direct deposit / credit / later     │
│                                                  │
│ [Finish setup]                                   │
└──────────────────────────────────────────────────┘
```

The wizard POSTs to `gateway-bff /api/v1/onboard/start` on mount (binds code → user), then `gateway-bff /api/v1/onboard/complete` on submit (persists defaults + issues the daemon's mTLS bundle).

Server-side flow:
1. `iogridd pair --request` mints a 6-char code, writes to `~/.iogrid/pairing-code.txt`, prints to stdout.
2. Browser hits `/onboard/<code>`, user signs in (Google or magic-link).
3. After sign-in the wizard calls BFF `/onboard/start { token }`.
4. User picks defaults, wizard calls BFF `/onboard/complete { token, defaults }`.
5. Daemon's background poll (`/onboard/poll { token, daemon_pubkey }`) flips from 202 to 200 with the bundle. Daemon writes the bundle to `~/Library/Application Support/iogrid/identity.pem` (Mac) / `~/.config/iogrid/identity.pem` (Linux) / `%APPDATA%\iogrid\identity.pem` (Windows) and starts the supervisor.
6. Browser confetti → redirect to `/provide` dashboard.

The pairing code is **single-use** + **10-min TTL** + **Crockford-base32 minus I/L/O/U** (32^6 ≈ 1B combinations, far above brute-force ceiling). Once claimed it's invalidated server-side.

---

## Auto-update

Daemon polls `https://updates.iogrid.org/manifest.json` every 24h (with ±10% jitter). The manifest is signed with Ed25519; the public key is embedded in every released binary. On a newer release the daemon:

1. Verifies the manifest's Ed25519 signature with the embedded pubkey.
2. Picks the highest semver above its own (respecting `min_supported_from` for upgrade hops).
3. Downloads the matching artifact for its rustc target triple.
4. Verifies SHA256 + (if present) cosign blob signature.
5. Writes to `<install>/iogridd.next`, atomic-renames over `<install>/iogridd`.
6. Triggers a restart via launchctl/systemd/sc-control. The OS service manager handles starting the new image.
7. Keeps `<install>/iogridd.prev` for 7 days. If the new binary fails its first health check the wrapper rolls back.

See `installer/auto-update/README.md` for the full spec.

---

## Uninstall

### Mac

```bash
sudo iogridd uninstall
# OR manually:
launchctl bootout gui/$(id -u)/org.iogrid.daemon
rm ~/Library/LaunchAgents/org.iogrid.daemon.plist
sudo rm /usr/local/iogrid/iogridd /usr/local/bin/iogridd
rm -rf ~/Library/Application\ Support/iogrid ~/Library/Logs/iogrid ~/.iogrid
```

### Linux (.deb)

```bash
sudo apt-get purge iogrid
# OR for the curl-pipe install:
systemctl --user disable --now iogridd.service
rm ~/.config/systemd/user/iogridd.service ~/.config/iogrid/config.toml
sudo rm /usr/local/bin/iogridd
rm -rf ~/.local/share/iogrid ~/.iogrid
```

### Windows

```powershell
# Control Panel → Programs → iogrid → Uninstall
# OR
msiexec /x "C:\path\to\iogrid-0.1.0-x64.msi"
```

After local uninstall, server-side data is purged after a 7-day grace period (so re-installing within 7 days resumes from the same identity).

---

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `launchctl bootstrap` returns 5 | Already loaded | Run `launchctl bootout gui/$(id -u)/org.iogrid.daemon` first |
| `systemctl --user` says "Failed to connect to bus" | XDG_RUNTIME_DIR not set in shell | `export XDG_RUNTIME_DIR=/run/user/$(id -u)` |
| Browser doesn't open after curl-pipe install | Headless server (`$DISPLAY` unset) | Visit the URL printed to terminal manually |
| `sc.exe create iogridd` says "service already exists" | Previous install left it | `sc.exe delete iogridd` then retry |
| Pairing code expires | 10-min TTL exceeded | Run `iogridd pair --request` to mint a fresh one |
| `iogridd pair --request` prints nothing | Daemon not running yet | Wait ~3s after install for service to come up |

---

## CI

`.github/workflows/installer-ci.yml` runs:
* `lint` — shellcheck + nfpm config validate on every PR
* `macos-pkg` (matrix: arm64 + amd64) — builds the .pkg on macos-latest, uploads as artifact
* `windows-msi` — builds the .msi on windows-latest, uploads as artifact
* `linux-packages` (matrix: amd64 + arm64) — builds .deb + .rpm + .apk via nfpm in a Ubuntu container
* `smoke-linux` — runs install.sh against a fake `releases.iogrid.org` mirror, verifies binary lands + pair code mints

Signing/notarisation only fires when the corresponding secret is present (set on the release-only runner). PRs from forks produce unsigned artifacts so contributors can manually smoke-test.
