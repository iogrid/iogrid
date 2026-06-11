# ADR 0001 — How iOS builds run on provider Macs

Status: accepted · 2026-06-11

## Context
iogrid runs customers' iOS builds on third-party Mac owners' machines. We need
isolation (untrusted customer build commands run on a stranger's Mac),
reproducibility (a pinned Xcode per job), and the smallest possible per-provider
footprint so onboarding a Mac is easy.

## Options
1. **Native / host-direct** — run `xcodebuild` directly on the host.
   - Lightest (no VM). BUT: zero isolation (customer code runs as the host
     user), and the build is stuck on whatever Xcode the host happens to have
     (this is what surfaced the "Swift 6.2 needs Xcode 16.4, host has 16.2"
     wall). Acceptable ONLY for first-party/trusted builds.
2. **Tart VM** — a full ephemeral macOS VM per build from a pre-baked image.
   - Full isolation + a version-pinned Xcode + deleted after each build.
   - Heavy ONLY because Apple forbids containerizing macOS: the sole isolation
     primitive Apple exposes is Virtualization.framework (a full VM), Apple
     Silicon only. GitHub Actions / Cirrus do exactly this for the same reason.
3. **Containers (podman/Docker)** — impossible for macOS/iOS. Containers can't
   run macOS. (They ARE right for the Linux compute/proxy workloads.)

## Decision
- **Untrusted, multi-tenant builds → Tart VM** (option 2). Isolation is
  mandatory; the host's Xcode version becomes irrelevant because the image
  carries a pinned one — so the version mismatch can never surface again.
- **First-party/trusted builds → native** is acceptable as a lighter fast path
  (`auto_runner()` already prefers Tart, falls back to native).
- **Apple Silicon required.** Intel Macs can't virtualize macOS → not supported
  as iOS-build providers.

## The footprint (what's in the ~80 GB, and how to shrink it)
The stock cirruslabs `macos-sequoia-xcode` image: macOS base ~15-20 GB + Xcode
~40 GB + iOS/watchOS/tvOS/visionOS simulators ~10-20 GB. **~40 GB of that is
non-iOS sims/SDKs.** A slim iogrid image (macOS + Xcode CLI + the iOS SDK +
one iOS simulator only) is ~30-35 GB. Open follow-up: bake + publish
`ghcr.io/emrahbaysal/iogrid-ios-builder:slim` to cut every provider's footprint
roughly in half.

## Per-provider prerequisites (the whole list)
Apple Silicon Mac · macOS 13+ · the iogrid app · **~35 GB free disk** (slim
image) — no manual Xcode, ever. Automated by `daemon/scripts/provision-mac-provider.sh`.

## Consequence for the dog-food (Hatice's M1)
After reclaiming iogrid's own build cruft it has ~21 GB free; her personal data
(separate user account) fills the rest of the 228 GB disk. 21 GB < ~35 GB, so a
real iOS build needs either freeing ~15 GB more of her data or a dedicated Mac
with ~100 GB free. The blocker is disk, not Xcode — and not a founder "decision"
about Xcode versions.
