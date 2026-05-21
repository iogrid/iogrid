# Windows side-by-side install root (Squirrel.Windows convention)

This directory documents the **install-root layout** the iogridd Windows
service expects once Squirrel.Windows auto-update is wired in (Phase 2 of
EPIC [#348](https://github.com/iogrid/iogrid/issues/348), tracked by
[#389](https://github.com/iogrid/iogrid/issues/389)).

The directory itself is intentionally empty in the source tree — it's a
**runtime convention**, not a payload. The `.msi` installer (`iogrid.wxs`)
lays down a starter `Update.exe` at `Program Files\iogrid\Update.exe` on
first install; thereafter Squirrel manages this tree on its own.

## Runtime layout

```
C:\Program Files\iogrid\
├── Update.exe                — Squirrel runtime (also re-emitted into each
│                                app-<version>\ as `Squirrel.exe` for
│                                rollback paths). Started by the SCM as
│                                the registered service ImagePath.
├── packages\
│   ├── RELEASES              — fetched from releases.iogrid.org/windows/RELEASES
│   ├── iogridd-0.1.0-full.nupkg     — initial install snapshot
│   ├── iogridd-0.1.1-delta.nupkg    — delta over 0.1.0 (when applicable)
│   └── iogridd-0.1.1-full.nupkg     — full snapshot mirror
├── app-0.1.0\                — initial install (from .msi)
│   ├── iogridd.exe
│   ├── LICENSE.txt
│   └── version.txt           — '0.1.0'
└── app-0.1.1\                — staged side-by-side by Update.exe --update
    ├── iogridd.exe
    ├── LICENSE.txt
    └── version.txt           — '0.1.1'
```

## How updates are applied (high level)

1. Daemon supervisor (or any equivalent trigger — Task Scheduler,
   `Update.exe --update <url>` invoked from a startup script) fetches
   `https://releases.iogrid.org/windows/RELEASES`.
2. Update.exe diffs the local installed-version line against the highest
   line in RELEASES.
3. If a new full / delta `.nupkg` is available, Update.exe downloads
   into `packages\`, verifies the SHA-1 (signed Authenticode signature
   on the .nupkg itself is verified separately against the Windows trust
   store).
4. The new payload is unpacked into `app-<new-version>\` next to the
   existing `app-<current-version>\`.
5. SCM is asked (via `sc.exe`) to stop `iogridd`, the service's
   `ImagePath` is repointed at the new `app-<new-version>\iogridd.exe`,
   and the service is restarted.
6. The old `app-<old-version>\` directory is retained for one cycle (so
   `Update.exe --rollback` can revert if the new binary crashes), then
   garbage-collected on the next successful update.

## Why this is split from the .msi

See `../squirrel/README.md` ("Squirrel integration approach — repack the
existing .msi binary") for the full rationale. Short version: the .msi
handles **first install** (UAC + service registration + Start Menu
shortcut); Squirrel handles **subsequent in-place binary swaps** without
re-prompting UAC.

## Sentinel kept under VCS

`.gitkeep` keeps this directory present in fresh clones so the .msi build
step doesn't fail with "directory not found". The directory has no other
contents at source-time.
