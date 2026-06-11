# ADR 0001 — How iOS builds run on provider Macs

Status: accepted · 2026-06-11

## Contents
- **Decision** — Tart VM (untrusted) vs native (trusted) vs containers (impossible for macOS)
- **Footprint** — what's in the ~80 GB, slimmable to ~35 GB
- **Per-provider prerequisites** — Apple Silicon · macOS 13+ · ~35 GB free
- **Add. 1** — customer-source confidentiality (the other threat direction) + the truth about stranger Macs
- **Add. 2** — avoiding the "two Xcodes" double footprint (route runner to provider type)
- **Add. 3** — **DX scorecard** (native ~82 vs dev-in-Tart ~50)
- **Add. 4** — the 2-VM kernel cap + the commercial-license **risk**
- **Add. 5** — **max-security stack** (no Apple-Silicon TEE → defense-in-depth)
- **Add. 6** — **security-measure applicability scorecard** (native vs Tart, per measure)
- **Add. 7** — sealed/encrypted VM: HW memory isolation correction (strong, not attestable)
- Plus the **native-vs-Tart runner scorecard** + **confidentiality scorecard** in the body.

Scripts: `daemon/scripts/provision-mac-provider.sh` (#722) · `bake-slim-ios-image.sh` (#724, unvalidated).

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

## Addendum 2 — avoiding the "two Xcodes" double footprint (match runner to person)

A Mac owner who is ALSO an iOS dev must not be forced to carry both a native
Xcode (~40 GB on the host) AND a Tart image (~35 GB) — those are separate
storage (~75 GB total; Tart's copy-on-write sharing only helps *between* Tart
VMs, not host↔VM). To avoid 2×, the owner picks ONE:

- **All-Tart:** do their own dev inside a persistent Tart VM; iogrid builds are
  throwaway clones of the SAME base image → one ~35 GB image + thin deltas.
  Catch: laggy GUI over screen-sharing + **no physical-device debugging** (no
  USB passthrough in macOS VMs). OK for simulator-only/casual devs, painful as a
  daily driver for serious iOS devs.
- **Reuse native:** if they already have Xcode, iogrid runs builds on it (native
  runner) → ZERO extra footprint. Lower isolation → trusted/OSS tier; the
  scheduler only routes builds whose required Xcode **matches** theirs (so the
  version mismatch still never surfaces).

**Decision — route the runner to the provider type:**
- *Already an iOS dev (has Xcode):* reuse native, one footprint, keeps device
  debugging, serves version-matched builds in the trusted/OSS pool.
- *Non-dev / wants isolation / version flexibility:* Tart (and may do light dev
  inside the same VM, still one footprint).

Net: nobody carries two Xcodes. The double only occurs if an owner insists on
*both* native-for-self and Tart-for-iogrid, which this routing makes unnecessary.

## Addendum 3 — developer-experience scorecard (a dev doing their OWN work)

For a Mac owner who is also an iOS dev deciding whether to live inside a Tart
VM vs work natively (0-100, higher = better):

| Dimension | Native | Dev-in-Tart |
|---|:--:|:--:|
| IDE/UI responsiveness | 95 | 45 |
| Physical iPhone debug (USB) | 95 | 10 |
| Simulator performance | 90 | 60 |
| Build / compile speed | 90 | 75 |
| Full macOS (Continuity/iCloud/notifications) | 95 | 40 |
| Peripherals (Touch ID/camera/displays/USB) | 95 | 25 |
| Reproducible / resettable env | 50 | 95 |
| Multiple Xcode versions side-by-side | 50 | 95 |
| Isolation (work doesn't pollute the Mac) | 40 | 90 |
| Setup effort | 70 | 60 |
| Single-env disk footprint | 65 | 70 |
| **Overall — daily interactive iOS dev** | **~82** | **~50** |
| **Overall — CI / sim-only / multi-version** | **~62** | **~80** |

Decisive for daily interactive dev: native (no physical-device debug in a VM,
laggy IDE over screen-share). Tart wins only for non-interactive/CI work. →
confirms "dev providers reuse native; non-devs run Tart."

## Addendum 4 — the 2-VM cap + commercial-license RISK
Apple enforces **max 2 macOS VMs per Mac** at the KERNEL level
(`hv_apple_isa_vm_quota`), from macOS license §2B(iii) ("up to two instances …
within virtual operating system environments … for software development,
testing, and personal **non-commercial** use").
- **Economic:** a provider Mac caps at **2 concurrent builds** — the per-Mac
  earnings ceiling, regardless of how powerful the Mac is.
- **Legal RISK (unresolved):** running macOS VMs commercially-for-hire on
  third-party *consumer* Macs is a gray area Apple polices. The legit Mac
  clouds (AWS EC2 Mac, MacStadium) use dedicated Apple hardware + special Apple
  agreements + 24h minimums to comply. Needs real legal review before scale;
  may force iOS-build providers into a vetted/contracted tier vs the open pool.

## Addendum 5 — MAXIMUM security for the customer's source (threat: malicious host)
Fundamental limit: code must be plaintext to compile; the provider owns the
hardware; **Apple Silicon has no workload TEE** (Secure Enclave guards keys, not
workloads), and FHE/MPC are too slow to compile. So **no cryptographic
confidentiality is possible on a stranger's Mac.** Max security = defense in
depth, biggest lever first:
1. **Tier providers (the only real lever):** sensitive code → trusted tier
   (iogrid-operated Apple HW, or KYC'd + bonded + NDA'd providers); open
   consumer pool only for OSS/non-sensitive.
2. **Customer keeps crown jewels:** signing keys/secrets NEVER leave the
   customer (sign customer-side / HSM). Stolen source can't ship as their app.
3. **Minimize exposure:** ephemeral VM; source decrypted only into VM
   memory/tmpfs (never plaintext on provider disk); egress locked to iogrid
   only; image-digest attestation (no silent backdoor of the build env).
4. **Economic+legal deterrence (uses the $GRID layer):** providers stake $GRID
   collateral → slashed + banned if caught snooping; KYC/NDA for the tier.
5. **Detection + accountability:** per-build canary watermarks unique to
   (customer, provider, build) → trace any leak to the provider → slash.

**Implementable now on every tier (no business decision needed):** #2
customer-side signing, #3 in-memory/no-disk source + egress lockdown +
attestation, #5 canary watermarks. #1 + #4-KYC are product/business decisions.
**Never market** cryptographic confidentiality on the open pool.

## Addendum 6 — security-measure applicability: native Xcode vs Tart (0-100)

How well each hardening measure can actually be APPLIED in each runner.

| Security measure | Native | Tart | Note |
|---|:--:|:--:|---|
| **Runner-dependent (Tart wins)** | | | |
| Ephemeral env (fresh + wiped per build) | 35 | 95 | Host persists state; clone/delete VM is ephemeral by design |
| Source never plaintext on provider disk | 25 | 70 | Host swap/Spotlight/TimeMachine capture vs VM tmpfs + ephemeral disk |
| Network egress lockdown (iogrid-only) | 30 | 90 | Firewalling the host hurts the owner; VM has its own controllable NIC |
| Build isolated from host filesystem | 20 | 85 | Native runs as host user (sees owner files + vice-versa); VM sandboxed |
| Multi-tenant safety (build A can't see B) | 30 | 95 | Shared host vs per-build VM |
| Build-env attestation (non-backdoored toolchain) | 20 | 70 | Can't attest a host's Xcode; can pin+measure a VM image digest |
| Instant kill + clean wipe of a bad build | 45 | 90 | Kill+clean vs delete the whole VM |
| **Fundamental limit (NEITHER solves)** | | | |
| Memory confidentiality (host can't dump build RAM) | 10 | 15 | No TEE on Apple Silicon; host owns the hardware/hypervisor |
| **Runner-independent (equal — the real deterrents)** | | | |
| Customer-side signing (keys never leave customer) | 90 | 90 | Applies regardless of runner |
| Per-build canary watermark (trace leaks) | 85 | 85 | Inject into source regardless of runner |
| $GRID staking / slashing deterrence | 85 | 85 | Economic layer, runner-independent |
| Provider KYC / trusted-tier routing | 85 | 85 | Business layer, runner-independent |
| **Overall — runner-dependent isolation only** | **~27** | **~85** | Tart dominates where the runner matters |
| **Overall — incl. runner-independent layers** | **~45** | **~75** | Economic/key/legal layers lift both equally |

**Three reads:**
1. Where the runner actually matters (ephemerality, egress, host-FS isolation,
   multi-tenant, attestation, kill/wipe), **Tart wins decisively (~85 vs ~27).**
2. The fundamental ceiling — **memory dump by the host — NEITHER solves** (~10-15
   both); no Apple-Silicon TEE. This is why truly secret code needs the trusted
   tier, not just a better runner.
3. The **decisive real-world protections are runner-independent** — customer-side
   signing, canary watermarks, $GRID staking/slashing, KYC/tiering. They protect
   equally under either runner, and they're where iogrid's actual security
   posture comes from given no runner can cryptographically stop a malicious host.

## Addendum 7 — CORRECTION: can we run a sealed/encrypted VM? (memory isolation revised)

Research correction to Addendum 6. I under-scored Tart's memory confidentiality.
- **Apple Silicon Virtualization.framework gives hardware-enforced guest-memory
  isolation:** a guest VM's RAM is NOT readable by ordinary host processes /
  EDR / casual scanning. Against the realistic provider threat this is strong.
  Revise the "Memory confidentiality" row: **native ~10, Tart ~50** (was 15) —
  strong isolation, not casual-readable.
- **But it is NOT formal confidential computing.** Apple does not expose
  (to third parties) memory ENCRYPTION with a host-excluded key, nor remote
  ATTESTATION — unlike AMD SEV-SNP / Intel TDX / AWS Nitro. So you cannot
  CRYPTOGRAPHICALLY PROVE to a customer/auditor that a *determined* owner
  (custom VMM on the low-level Hypervisor.framework, or kernel tooling) can't
  extract guest memory. Apple HAS attestable confidential computing on Apple
  Silicon — **Private Cloud Compute** — but it's internal-only, not a VF feature.

**A "completely sealed lockdown encrypted VM" on a stranger's Mac:** buildable
and STRONG (encrypted disk + HW memory isolation + no SSH/console + egress
lockdown + ephemeral wipe) — enough for most proprietary code against the
realistic threat. NOT cryptographically provable / attestable against a
determined expert → crown-jewel code still needs the trusted tier (iogrid HW),
until/unless Apple exposes PCC-style confidential VMs to third parties.

## Addendum 8 — DECISION: drop native for the untrusted pool (native-max vs sealed-VM)

The question: if a provider wants to use his OWN native Xcode, what's the max
customer-source security vs a sealed VM — and do we keep native?

**Structural fact:** native has **no isolation boundary** between the build and
the Mac owner (the owner is root on the machine the build runs on). A sealed VM
has a real one. You cannot harden native into having a boundary.

| Approach (max-hardened) | What you can do | Customer-source confidentiality |
|---|---|:--:|
| **Native** | dedicated restricted build user + sandbox-exec/Seatbelt (FS+net) + best-effort wipe | **~20** — owner is root → reads the build user's files + dumps its memory. Hardening protects the PROVIDER from customer code, not the customer from the provider. |
| **Sealed VM** | encrypted disk + HW guest-memory isolation + egress lockdown + ephemeral wipe + attestation | **~60** — a real boundary the owner must actively defeat. Strong, not cryptographically provable. |
| **Trusted tier (iogrid HW)** | the host IS trusted | **~95** |

**DECISION:**
- **Drop native for the untrusted / open provider pool — Tart-only there.** Native
  offers ~no protection from the very provider you don't trust, plus the Xcode
  version-mismatch trap. No reason to keep it for customer builds on strangers' Macs.
- **Keep native only as a trusted/first-party fast path** (iogrid-operated Macs, the
  dog-food, or a bonded trusted tier where the owner is already trusted) — there it's
  lighter and fine.
- A provider's own native Xcode is for HIS work; to host CUSTOMERS' builds he runs
  Tart (sealed). The two coexist with no conflict.

Consequence: `auto_runner()`'s native fallback (#727 warns on it) should be
gated to trusted providers only; the open pool requires a provisioned Tart
toolchain (provision-mac-provider.sh / provision::ensure_provisioned).

## Addendum 9 — consolidated "can the Mac owner access X?" comparison

One table answering the operator's actual question per approach (max-hardened).

| Can the Mac owner access… / capability | Native (host) | Sealed VM (Tart + hardening) | Trusted tier (iogrid HW) |
|---|---|---|---|
| **Customer source code** | **YES** — plain files in the home dir, trivially | **Hard** — must mount the VM disk / dump guest RAM (HW-isolated) | **No** — you control the host |
| **Build artifacts (.ipa)** | YES — on host disk | Hard — inside the VM, wiped after | No |
| **Signing credentials / certs** | n/a — **never sent** (customer-side/HSM signing) | n/a — never sent | n/a — never sent |
| **Build process memory** | YES — root reads any process | Hard — Apple-Silicon HW guest-memory isolation | No |
| Full-disk encryption at rest | weak — owner holds the key | yes — encrypted VM disk | yes |
| Network egress control | weak — host network, owner can change rules | strong — VM's own NIC, locked to iogrid | strong |
| Filesystem sealing (build ↔ host) | none — same user/FS | strong — VM sandbox | strong |
| Attestation (prove env un-tampered) | no — can't attest a host's Xcode | partial — pin/measure image digest | yes — you run it |
| Memory encryption / TEE | no | no — Apple exposes none to 3rd parties | n/a — host is trusted |
| Ephemeral / wiped per build | no — host persists state | yes — VM deleted | yes |
| **Customer-source confidentiality (0-100)** | **~20** | **~60** | **~95** |

**The one-line answer:** on native the owner can read **everything** (source,
artifacts, memory) trivially; a sealed VM blocks casual access and forces active
introspection (strong, not provable); only the trusted tier (host you control)
is safe for crown-jewel source. → **Tart-only for the untrusted pool; native
gated to trusted; trusted tier for secret source** (Addendum 8).

## Addendum 10 — the host-macOS-version constraint (a third provider-eligibility axis)

Discovered live baking the dog-food Mac (2026-06-11): the bake failed with
`host macOS version is outdated to run this virtual machine`. The rule is
Apple Virtualization.framework's, not Tart's: **a macOS guest VM requires the
host macOS to be ≥ the guest macOS.** You cannot run a newer macOS guest on an
older host.

This adds a THIRD hard eligibility axis for an iOS-build provider, alongside
"Apple Silicon" and "enough disk":

| Host macOS | Max guest macOS | Max Xcode (→ max iOS SDK) |
|---|---|---|
| Sonoma (14) | Sonoma (14) | **Xcode 16.2** |
| Sequoia (15) | Sequoia (15) | Xcode 26.x |
| Tahoe (26) | Tahoe (26) | Xcode 26.x+ |

**Why it bites:** Apple has required the iOS 26 SDK (Xcode 26+) for App Store
uploads since ~2026-04. So a customer who needs an uploadable build can ONLY
be served by a provider whose host is **Sequoia or newer**. A Sonoma provider
can compile/test but cannot produce a current-SDK shippable build. The
dog-food Mac (Hatice's M1) is on Sonoma 14.6.1 → it proves the pipeline with
Xcode 16.2 but needs a host OS upgrade for production parity.

**Consequences:**
- **Scheduling:** the daemon must advertise `host_macos_version` → derived
  `max_xcode`, and workloads-svc must route a job only to providers whose
  max_xcode ≥ the job's required Xcode. Without this, jobs fail at build time
  on under-versioned hosts. (New capability field; follow-up issue.)
- **Trusted tier:** iogrid-operated build Macs must be kept on a current macOS
  to serve current-SDK customers — an ongoing ops cost, not a one-time setup.
- **Open pool fragmentation:** provider macOS-version diversity partitions the
  fleet by buildable-Xcode; the effective capacity for the newest SDK is only
  the Sequoia+ subset. Onboarding should surface "upgrade macOS to earn from
  current-SDK jobs."
- **Security note:** none of this changes the isolation model (Addenda 5–9);
  it constrains WHICH providers can take WHICH jobs. The sealed-VM guarantees
  are identical across host versions.
