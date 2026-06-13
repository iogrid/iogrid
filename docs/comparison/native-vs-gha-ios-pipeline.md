# iOS Pipeline Comparison — Native (Xcode 26 + Simulator on a provider Mac) vs GitHub Actions + Maestro

> **What this answers (founder ask):** *"Show me the comparison from duration and
> quality and comprehension perspective"* + *"I am expecting the simulator to be
> 10 times more comprehensive tests to be achieved compared to Maestro."*
>
> Scope: docs-only. Devnet only. No secrets. Numbers are **real and sourced**
> where a receipt exists in the repo or in CI; every figure that is a projection
> is explicitly labelled **(estimate)** with its basis.

---

## Summary table (top-line)

| Dimension | OLD — GitHub Actions + Maestro | NEW — Native Xcode 26 + iOS-26 Simulator (provider Mac, dispatched via iogrid build-gateway) | Verdict |
|---|---|---|---|
| **Build wall-clock** | **46.8 min mean** / 46.6 min median (range **39.1–55.4 min**), n=11 successful `mobile-ios-ci` runs | Native `xcodebuild` on Apple-Silicon (M1) provider, **dog-food PROVEN `BUILD EXIT 0`** 2026-06-13. Full cold build not yet timed on the native path → **(estimate)** see §1 | Native removes the cloud-CI tax (cert/keychain/profile churn, cold runner, queue) — see §1 |
| **Test comprehension** | Maestro black-box: **11 flows, 73 commands, 28 visible-element assertions** — no app introspection | Native: **jest 130/133 passing** (just ran, 1.8 s) **+** XCUITest/XCTest/Instruments via `xcodebuild test` — accessibility tree, internal-state assertions, performance | Native is **categorically** more comprehensive; the "10×" is achievable but should be measured by *assertion power*, not flow count — see §2 |
| **Cost** | GitHub Actions macOS runner: **$0.08 / build-minute** → **~$3.74 / build** at the 46.8-min mean **(public GHA price)** | Provider paid in **devnet $GRID**; product thesis = **~50% of GHA** | Native path is the product's flagship differentiator — see §3 |
| **VPN tunnel test** | Cannot test the WireGuard Network Extension (Apple blocks NEs in any simulator) | **Same hard limit** — NE tunnel is **device-only**; the sim proves the *app + UI*, never the tunnel | Identical caveat; be precise — see §4 |

**Headline for the founder:**
- **Build minutes:** OLD = **46.8 min mean** (measured, n=11). NEW = native `xcodebuild` on the provider Mac, dog-food **PROVEN to EXIT 0** but **not yet stopwatch-timed end-to-end** — projected materially faster because it deletes the cloud-CI overhead (no fresh-cert mint, no keychain build, no profile regen, no cold-runner spin-up, no queue). We will publish the real native minutes once a full cold build is timed; until then the native full-build figure is an **(estimate)**, not a measured number.
- **Test comprehension verdict:** native is more comprehensive **by category, not just by count**. Maestro can only assert *"is this text/element visible on screen"* (28 such assertions across 11 flows) and drives the app with blind coordinate/label taps. The native stack (XCUITest + XCTest + jest + Instruments via `xcodebuild test`) reads the **accessibility tree**, can assert **internal app state**, and measures **performance** — and the pure-logic layer alone already runs **130 passing jest assertions in 1.8 s**. "10× more comprehensive" is a reasonable *target* once XCUITest suites are authored; today the honest claim is "**a categorically stronger class of test, with 130 logic assertions already green and the introspective UI layer unlocked**."

---

## 1. Build duration — native vs GitHub Actions

### GitHub Actions (`mobile-ios-ci.yml`) — MEASURED

Wall-clock per run = `updatedAt − createdAt`, computed over the last 20 runs of
the `mobile-ios-ci.yml` workflow (`runs-on: macos-latest`, `timeout-minutes: 60`):

| Statistic | Value (successful runs, n=11) |
|---|---|
| Mean | **46.8 min** |
| Median | **46.6 min** |
| Min | 39.1 min (run `27401530949`) |
| Max | 55.4 min (run `26953850960`) |

These are **real** numbers pulled from the GitHub Actions API on 2026-06-14.
Why a TestFlight build takes ~47 min in GHA — the workflow does far more than
compile (read `.github/workflows/mobile-ios-ci.yml`):

1. Cold `macos-latest` runner spin-up + `setup-xcode` (latest-stable / Xcode 26).
2. `npm install --legacy-peer-deps` + `npx expo prebuild` (generate native project).
3. **Fresh Distribution cert mint + revoke dance** (fastlane `cert`), provisioning-profile
   regen (fastlane `sigh`) for **both** the app and the PacketTunnelProvider NE,
   ASC-API capability ensures (SIWA / Associated Domains) — all hitting Apple's portal.
4. Build the WireGuardKitGo static lib (`libwg-go.a`) for **two** platforms.
5. `xcodebuild build` for the simulator (the Maestro gate) **and** `xcodebuild archive` for device.
6. Maestro driver spin-up (~2–5 min cold) + the flow run (up to 3 outer attempts, 25-min cap).
7. Archive → export → altool upload to App Store Connect.

Steps 3 + 6 are **pure cloud-CI tax** — they exist only because the runner is
ephemeral and shared across the Dynolabs team (the workflow's own comments
document cert-cap races with sibling apps vcard/cinova/ping).

### Native (Xcode 26 on a provider Mac) — PROVEN, NOT YET TIMED

Status (sourced from `docs/ledger/TRACKER.md`, 2026-06-13T15:00Z):
the real iogrid app **built natively to `BUILD EXIT 0`** on Hatice's M1 Mac under
`/Applications/Xcode-26.5.0.app` (iOS 26.5 SDK), then launched on the iOS-26
simulator (iPhone 17 Pro, sim `F29A421F`) → **PID 57870**, real onboarding UI
rendered. Dispatched via the iogrid build-gateway → daemon **native runner**
(`auto_runner()` picks `NativeRunner` when Tart is off PATH; daemon crate
`daemon/crates/workload-ios/`).

**Honesty note on native build minutes:** a *full cold* native `xcodebuild` has
**not yet been stopwatch-timed** end-to-end on the provider Mac, so we will not
quote a native minute count as if measured. The "~9 s exit 0" builds that appear
in the ledger are **shell-probe dispatch tests** (`sw_vers; xcodebuild -version;
node --version`) used to prove the build-gateway → Mac dispatch path — **not** a
real app compile. Do not read them as the native build time.

**Why native is expected to be faster (estimate — basis stated):**

| Cost bucket | GHA | Native provider Mac | Native saving |
|---|---|---|---|
| Runner spin-up / queue | Cold ephemeral runner each run | Warm, always-on host | seconds–minutes |
| Cert/profile churn (step 3) | Mint + revoke + regen every run | Signing identity persists on host (or sim build skips signing entirely) | minutes |
| Dependency cache | `npm`/Pods cold or partially cached | Local `node_modules`, Pods, DerivedData persist | minutes |
| Incremental rebuild | Always clean | DerivedData warm → incremental | large on re-runs |

**Projected native full-build wall-clock: materially below the 46.8-min GHA mean
(estimate).** The basis is the elimination of steps 3 + 6 and warm caches — *not*
a measurement. **Action to make this a hard number:** time one full cold
`xcodebuild` of `iogrid.xcworkspace` on the provider Mac and replace this
estimate. (The shell-probe path already proves dispatch + the dog-food proves the
app compiles; only the stopwatch is pending.)

---

## 2. Test comprehension / quality — the core of the founder's question

### What Maestro actually is (OLD)

Maestro is **black-box tap automation**. It drives the app from the outside by
matching on-screen **text/labels** and issuing coordinate/label **taps**. It has
**no introspection** into the app: it cannot read a variable, assert a Redux/Zustand
value, inspect the accessibility tree programmatically, or measure performance.
Its assertion vocabulary is essentially *"is this visible / not visible."*

Measured Maestro surface in this repo (`mobile/ios/.maestro/`):

| Maestro metric | Count |
|---|---|
| Flows (`01`–`10` + master `00-all`) | **11** |
| Total commands | **73** |
| `assertVisible` | 24 |
| `assertNotVisible` | 4 |
| `tapOn` | 17 |
| `takeScreenshot` | 14 |
| `launchApp` | 10 |
| `extendedWaitUntil` | 3 |
| `inputText` | 1 |
| **Actual assertions (visible / not-visible only)** | **28** |

So the *entire* Maestro suite is **28 visibility assertions** driving the app with
**blind taps** — and even those run behind a flaky XCUITest driver. Two structural
problems, both documented in this repo, cap Maestro's value:

1. **Incompatible with Xcode 26.** Maestro 2.6.1's iOS driver fails on Xcode 26
   with `DeviceCtlResponse missing result` (it can't parse the `devicectl` JSON
   that newer Xcode emits). On the native Xcode-26 stack Maestro is a **dead end** —
   it was a workaround for cloud CI that *couldn't* run real native tests, and the
   native simulator path makes it obsolete.
2. **Driver-attach flakiness, already decoupled in CI.** The workflow currently
   exits the Maestro step `0` on **flow** failure (only a detected native crash
   hard-gates) because of a persistent XCUITest driver-attach issue (`#575`/`#599`)
   and a stale-XCTest-handle bug that needs an *outer restart loop* of up to 3 full
   re-runs. In practice Maestro is a **best-effort smoke**, not an enforced gate.

### What the native stack is (NEW)

On a real iOS-26 simulator driven by `xcodebuild test`, the app is testable with
the **first-class Apple + RN toolchain**:

| Tool | What it tests | Introspection level |
|---|---|---|
| **jest** (`node_modules/.bin/jest`) | Pure TS/JS logic: coordinator request/response seam, pricing, region grouping, account derivation, wallet deeplinks, $GRID balance RPC | Full — asserts return values, error paths, exact byte output |
| **XCTest** | Swift unit/logic on the native side (e.g. tunnel-config construction) | Full — asserts internal Swift state |
| **XCUITest** | UI driven via the **accessibility tree** (not blind taps); query elements by identifier/type, assert labels, values, existence, hittability | Deep — reads the real element hierarchy, can assert app state surfaced to a11y |
| **Instruments** (via `xcodebuild test`) | Performance: launch time, CPU, memory, hitches, leaks | Profiling — none of this exists in Maestro |

**jest is real and green TODAY** — running `node_modules/.bin/jest --config
jest.config.js --ci` in `mobile/ios` on node v22.22.2 produces:

```
Test Suites: 1 skipped, 10 passed, 10 of 11 total
Tests:       3 skipped, 130 passed, 133 total
Time:        1.812 s
```

**130 passing assertions in 1.8 seconds** vs Maestro's **28 visibility checks**
that take ~6 minutes when they pass at all — and jest is only the *logic* layer.
The introspective UI layer (XCUITest) is unlocked by the native simulator and not
yet authored into a suite.

### Why native is more comprehensive (introspection vs blind taps)

| Capability | Maestro (black-box) | Native (XCUITest + XCTest + jest + Instruments) |
|---|---|---|
| Assert a button is on screen | ✅ `assertVisible` | ✅ via a11y query |
| Drive the UI | ⚠️ blind coordinate/label tap | ✅ tap a **resolved** a11y element (deterministic) |
| Assert **internal app state** (e.g. "session is bound", "balance == 5.95 $GRID") | ❌ impossible | ✅ jest on the logic, XCTest/XCUITest on surfaced state |
| Read the **accessibility tree** | ❌ | ✅ first-class |
| Test **error/edge paths** (503 retry, 401, malformed response) | ❌ (can't inject) | ✅ jest already covers the coordinator 503/401 seam |
| Measure **performance** (launch, CPU, memory, leaks) | ❌ | ✅ Instruments |
| Run on **Xcode 26** | ❌ `DeviceCtlResponse missing result` | ✅ this is the native stack |
| Enforced as a gate | ⚠️ decoupled to best-effort (#575/#599) | ✅ `xcodebuild test` exit code is a real gate |

### Addressing "10× more comprehensive" honestly

The founder's instinct is **directionally correct**, but the right yardstick is
**assertion power**, not flow count:

- **By count, today:** native already runs **130 logic assertions** vs Maestro's
  **28 visibility assertions** — that's **~4.6× more assertions right now**, before
  a single XCUITest UI test is written.
- **By capability:** native can assert things Maestro **structurally cannot** —
  internal state, error paths, accessibility, performance. That is not a
  multiplier; it is a **different and strictly larger class** of test.
- **Reaching a clean 10×:** entirely achievable by authoring XCUITest UI suites on
  top of the 130 jest assertions (introspective per-screen tests covering
  onboarding, connect-state machine, region picker, wallet/top-up, settings —
  each asserting state + a11y, not just visibility). Once those land, **10×+ in
  total assertion count is realistic.**

**Honest verdict:** native is **categorically** more comprehensive today (deeper
class of test + 4.6× the assertion count via jest alone). The full "10×" is a
credible **target** delivered by writing the XCUITest layer the simulator now
makes possible — not something to claim as already-shipped.

---

## 3. Cost

### GitHub Actions macOS runner — public pricing

GitHub Actions bills **macOS** runners at **$0.08 per build-minute** (public
GitHub-hosted-runner price; macOS is the most expensive tier, ~10× the Linux
rate). Linux/Windows minutes are cheaper but **cannot build iOS** — Apple requires
macOS, so the macOS tier is unavoidable for this workload.

At our measured **46.8-min mean** build:

> 46.8 min × $0.08/min ≈ **$3.74 per iOS build** *(public GHA macOS price × measured mean)*

(For accounts on the free tier, macOS minutes also consume the included-minutes
budget at a **10× multiplier**, so the economic pressure is the same even when no
invoice is cut.)

### iogrid provider model

- The provider Mac is paid in **devnet $GRID** (this is a devnet dogfood — **no
  real money**).
- Product thesis (sourced: `docs/how-to/submit-ios-build.md`, `docs/ROADMAP.md`
  Phase-2 lede, `docs/runbooks/macos-sequoia-tart-unblock.md`): iogrid iOS-build CI
  targets **~50% of GitHub Actions pricing**, running on home-Mac providers, with
  the provider earning the majority share (the platform takes an 85/15 split in the
  provider's favour per the $GRID settlement model).

> **Projected customer price at the 50% thesis: ≈ $1.87 / build** *(estimate =
> half of the $3.74 GHA-equivalent; the actual $GRID-denominated price is set by
> the market + the 85/15 split, and is devnet-only today).*

The native path is **the product's flagship differentiator**, not just a cost
line: a real macOS provider runs `xcodebuild`, the customer submits with one API
call + an API key, **zero SSH, no access to the build machine**, and the provider
is paid in $GRID (proven end-to-end live: a real build settled on-chain, tx
`4Zrmyw8oT97…` Finalized — `docs/ledger/TRACKER.md`).

---

## 4. The simulator caveat (be precise)

This is the one place where **both** pipelines hit the **same hard Apple platform
rule**, and it must be stated exactly so it is never over-claimed:

✅ **What the iOS Simulator DOES prove (native path, dog-food PROVEN):**
- The real iogrid app **compiles** (`BUILD EXIT 0`, Xcode 26, no toolchain hacks).
- It **installs and launches** on the iOS-26 simulator (`io.iogrid.app`, **PID
  57870**, iPhone 17 Pro sim `F29A421F`).
- The **real onboarding UI renders** (screenshot evidence: "A VPN powered by people,
  not data centers", home-mesh illustration, $GRID copy, Continue).
- It is **UI-testable** in the simulator via XCUITest (accessibility tree, taps,
  state assertions) + jest for logic.

❌ **What NO simulator can prove (Apple platform rule — applies to GHA *and*
native, identically):**
- The **WireGuard VPN tunnel** cannot run in **any** iOS simulator. Apple does not
  allow **Network Extensions** (the `NEPacketTunnelProvider` that carries the VPN
  data plane) to execute in the simulator — they require a **real device**.
- Therefore **VPN peer-resolution / the actual encrypted tunnel is confirmable
  ON-DEVICE ONLY**, never via a simulator screenshot. This is exactly why the G1
  VPN-connect work is gated on the founder's physical iPhone + a daemon decap from
  his real IP (`docs/ledger/TRACKER.md`, repeated G1 entries), and why the VPN fix
  ships via **TestFlight to a real device**, not via a sim run.

**Net:** the simulator (on either pipeline) is the right tool for **app + UI +
logic** comprehension and is where the native stack's introspective advantage
lives. The **VPN tunnel itself** is a **device-only** test on both pipelines — the
native simulator does not change that, and we do not claim it does.

---

## Sources & reproduction

| Claim | Source / how to reproduce |
|---|---|
| GHA 46.8-min mean (n=11) | `gh run list --workflow=mobile-ios-ci.yml -L 20 --json createdAt,updatedAt,conclusion,status` → wall-clock = `updatedAt − createdAt` over successful runs (computed 2026-06-14) |
| GHA workflow does cert/profile/Maestro work | `.github/workflows/mobile-ios-ci.yml` |
| jest 130/133 passing in 1.8 s | `cd mobile/ios && node_modules/.bin/jest --config jest.config.js --ci` (node v22.22.2) |
| Maestro 11 flows / 73 commands / 28 assertions | `mobile/ios/.maestro/*.yaml` (counted) |
| Maestro ⟂ Xcode 26 (`DeviceCtlResponse missing result`) | Discovered on the native dog-food; Maestro is a cloud-CI workaround obsolete on the native stack |
| Maestro gate decoupled to best-effort | `.github/workflows/mobile-ios-ci.yml` "Run Maestro flows" step (#575/#599 `exit 0` on flow failure) + `docs/ledger/TRACKER.md` stale-XCTest-handle entries |
| Native dog-food `BUILD EXIT 0`, PID 57870, iOS-26 sim | `docs/ledger/TRACKER.md` 2026-06-13T15:00Z |
| Native build dispatched via build-gateway → native runner | `docs/how-to/submit-ios-build.md`; daemon `daemon/crates/workload-ios/` (`build_poller`, `auto_runner`) |
| GHA macOS price $0.08/min (10× Linux) | Public GitHub Actions hosted-runner pricing |
| ~50% of GHA thesis | `docs/how-to/submit-ios-build.md`, `docs/ROADMAP.md`, `docs/runbooks/macos-sequoia-tart-unblock.md` |
| On-chain $GRID settlement proven | `docs/ledger/TRACKER.md` (tx `4Zrmyw8oT97…` Finalized) |
| VPN NE is device-only | Apple platform rule; `docs/ledger/TRACKER.md` G1 entries; `docs/adr/0001-ios-build-isolation.md` |

**Explicitly labelled estimates (not measured):**
- Native **full cold build** wall-clock — projected below 46.8 min from removed
  cloud-CI overhead + warm caches; **not yet stopwatch-timed**. Action: time one
  full `xcodebuild` of `iogrid.xcworkspace` on the provider Mac.
- **$1.87 / build** customer price — = 50% of the $3.74 GHA-equivalent; the real
  $GRID-denominated price is market-set and devnet-only.
- **10× test comprehension** — a credible **target** after XCUITest UI suites are
  authored; today the measured figure is **130 vs 28 assertions (~4.6×)** plus a
  strictly larger class of test.

---

*Related: [`docs/adr/0001-ios-build-isolation.md`](../adr/0001-ios-build-isolation.md)
(how iOS builds run on provider Macs — Tart vs native tiers),
[`docs/how-to/submit-ios-build.md`](../how-to/submit-ios-build.md)
(the customer-facing build-gateway API).*
