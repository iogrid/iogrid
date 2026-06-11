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

## Addendum — confidentiality of the CUSTOMER's source (the other threat direction)

There are TWO opposite threats, and they need different answers:
- **Provider host ← malicious customer build.** Tart's ephemeral VM solves this well.
- **Customer source ← malicious Mac owner.** This is harder, and the honest truth:
  **neither native nor Tart gives a cryptographic guarantee on a stranger's Mac**,
  because the build runs on hardware the provider physically controls.
  - *Native:* the cloned source is plain files in the provider's home dir. The
    owner reads them trivially. Confidentiality ≈ nil.
  - *Tart:* the source lives inside the guest VM — but the VM's disk is a `.tart`
    bundle ON the host filesystem (host owner can mount it) and the host owns the
    hypervisor (can snapshot/dump guest memory). So a *determined, technical*
    owner can still extract it. Tart raises the bar from "sitting in plain sight"
    to "you must actively introspect a VM," but it is obscurity + effort, not a
    TEE. Apple Silicon has no general confidential-computing mode (the Secure
    Enclave is not a workload TEE like SGX/SEV/Nitro), so there is no hardware
    fix available on Mac.

**Therefore the protection for sensitive source is NOT the runner — it is the
trust layer:** vetted + economically-bonded (staked) providers, ToS forbidding
introspection, reputation/slashing on misbehavior, AND a **trusted-provider tier**
(iogrid-operated or bonded Macs) that customers with proprietary code opt into.
OSS / non-sensitive builds can use the open provider pool; sensitive apps use the
trusted tier. Signing identity never leaves the customer regardless (the IPA is
signed customer-side / via a secure signing step), so a provider can never steal
the signing key even if they see source.

## Can a Mac owner who uses Xcode for their own work also run Tart? Yes.
Host Xcode and Tart VMs are independent. The owner keeps using their host Xcode
normally; iogrid builds run in separate throwaway VMs that carry their own pinned
Xcode and never touch (or depend on) the host's. No conflict.

## Scorecard — native vs Tart (0-100; higher = better)

| Dimension (weight)                          | Native | Tart | Note |
|---------------------------------------------|:------:|:----:|------|
| Customer-source confidentiality (high)      |   15   |  55  | Neither is cryptographic on a stranger's Mac; Tart needs active VM introspection to steal vs plain files |
| Provider-host safety from customer code (hi)|   15   |  90  | Native runs customer code as the host user; Tart = ephemeral VM |
| Never surfaces Xcode version mismatch (high)|   10   | 100  | Only Tart pins Xcode in the image |
| Reproducibility                             |   20   |  95  | Host's Xcode vs a pinned image |
| Scales to thousands of providers            |   25   |  92  | Version chaos + no isolation vs uniform |
| Disk footprint (lighter better)             |   85   |  35  | Just host Xcode vs a 35-80 GB image |
| Build performance                           |   90   |  75  | Bare metal vs ~10-20% VM overhead + boot |
| Provider onboarding                         |   55   |  75  | Needs the right Xcode (manual) vs automated but a big one-time pull |
| Concurrency per Mac                         |   40   |  70  | ~1 shared host vs up to 2 VMs (Apple license cap) |
| **Weighted overall (untrusted network)**    | **~33**|**~80**| Tart wins decisively where providers are untrusted |
| **Weighted overall (trusted/first-party)**  | **~70**|**~72**| Roughly a tie when you already trust the host; native is lighter/faster |

**Read:** for the public untrusted network → **Tart**, plus the trust/economic
layer + a trusted tier for sensitive source. For first-party/dog-food on a Mac you
already control → native is fine and lighter. Crucially, **no runner makes a random
stranger's Mac safe for truly secret source** — that requires the trusted tier.
