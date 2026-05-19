# iogrid-platform-mac

macOS-specific support code for the iogrid daemon:

- HID idle detection (no `IOKit` FFI — shells out to `ioreg` so the static binary stays `forbid(unsafe_code)`).
- LaunchAgent install / paths.
- Self-update binary swap with SHA-256 verification.
- macOS version detection — `macos_major_version()` + `supports_ios_build()`.

## macOS version requirements

| Capability | Minimum macOS |
|---|---|
| Daemon proper (bandwidth + idle-detect + UI bridge) | macOS 13 Ventura |
| Docker workloads via Docker Desktop | macOS 13 Ventura (Docker Desktop's own minimum) |
| **iOS-build workloads via Tart** (see `iogrid-workload-ios`) | **macOS 15 Sequoia** |

`supports_ios_build()` shells out to `sw_vers -productVersion`, parses the major version, and returns `true` iff that integer is `>= 15`. The supervisor checks this on startup and must NOT advertise `IOS_BUILD` as an eligible workload type on older hosts — otherwise the coordinator could send an assignment that the daemon can only ever reject.

The iOS-build runner additionally needs the following CLI tools on `$PATH`:

- `tart` — install via `brew install cirruslabs/cli/tart`
- `sshpass` — install via `brew install hudochenkov/sshpass/sshpass`
- `curl` — system-provided

The Tart default VM credentials (`admin` / `admin`) are hard-wired in `iogrid_workload_ios::TartRunner::default()`. Customers who use a different base image must override these via the `ssh_user` + `ssh_password` fields.
