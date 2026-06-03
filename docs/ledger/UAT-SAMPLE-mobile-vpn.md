# UAT — iogrid iOS app (VPN) — 2026-06-03

> **Standard User-Acceptance-Test walk.** *(A **filled DEMONSTRATION sample** of [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md) for the **native iOS app**. It shows how a UAT executor walks the iogrid VPN app — including phone-only steps: Sign in with Apple, the iOS "Add VPN Configurations" system dialog, region selection, and Ping top-up.)*
>
> ⚠️ **This is a demonstration, not a real device walk.** No physical iPhone / TestFlight build was walked for this sample, so the result marks below are **illustrative** of the format and every evidence image is a clearly-labeled **`SAMPLE / PLACEHOLDER — not a real capture`** PNG. When iogrid runs a real iOS UAT, replace each placeholder with an actual device screenshot and re-judge each row from what the tester sees. Screen names, `testID`s, the bundle id, and the deep-link scheme below are **real** (read from `mobile/ios/`).
>
> **Golden rule — this document is 100% the end-user's experience.** Every step is a real **tap / type / swipe** on the shipped app. No terminal, no API, no code reading. A non-technical beta tester holding the phone must be able to follow every row verbatim.

---

## Metadata

| Field | Value |
|---|---|
| **Product / release** | iogrid iOS 1.0 (VPN — consume-only) |
| **Build under test** | `io.iogrid.app` build 1 *(TestFlight link goes here when an external build is live — see [#574](https://github.com/iogrid/iogrid/issues/574))* |
| **Environment** | iogrid production VPN backend |
| **Surface(s)** | Native **iOS app** — *target device e.g. iPhone 15 / iOS 18.2* |
| **Tester** | `@p0-474-lonely` (UAT executor — demonstration, no live device) |
| **Walk date** | 2026-06-03 |
| **Overall verdict** | ⬜ NOT STARTED *(demonstration only — illustrative marks; re-walk on a real device to set a true verdict)* |

---

## How to read & fill this document

**Result legend:** ✅ PASS *(device screenshot required)* · ❌ FAIL *(file defect, leave issue open)* · ⛔ BLOCKED *(couldn't attempt)* · ⏭️ N/A · ☐ NOT WALKED.

Mobile rules: the walk **starts at install/launch** (no URL bar); name each screen the way a user would, with the dev `testID` in parens for engineers; Sign in with Apple, the iOS **Add VPN Configurations** dialog, and Ping top-up are each their **own steps**; evidence is a **device screenshot** committed under [`evidence-mobile/`](evidence-mobile). The executor is read-only on the app and never closes the issue.

---

## Test journeys

### TC-10 — First-run onboarding (install → signed in with Apple)

- **Persona:** New user on their personal iPhone who just installed iogrid for free VPN.
- **Goal (user's words):** *"As a new user, I want to install the app and sign in with Apple, so that I reach the VPN screen."*
- **Surface:** iOS app, build 1, *iPhone 15 / iOS 18.2 (target)*.
- **Preconditions:** App installed from TestFlight; signed out; an Apple ID active on the device.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | Home screen (springboard) | Tap the **iogrid** icon | Splash → app opens on the **Welcome** screen | ✅ | [📷 tc10-1-app-launch](evidence-mobile/tc10-1-app-launch.png) |
| 2 | Welcome | Tap **Continue** (`testID: onboarding-welcome-continue`) | Welcome carousel advances; option to **Skip** is present | ✅ | [📷 tc10-2-welcome](evidence-mobile/tc10-2-welcome.png) |
| 3 | Sign in | Tap **Sign in with Apple** (`testID: sign-in-with-apple-button`) | The iOS **Sign in with Apple** system sheet appears (Face ID) | ✅ | [📷 tc10-3-sign-in-apple](evidence-mobile/tc10-3-sign-in-apple.png) |
| 4 | Apple sheet | Complete the Face ID scan | Lands on the **Home** screen, signed in, with a **balance** card visible | ✅ | [📷 tc10-4-home](evidence-mobile/tc10-4-home.png) |

- **Journey verdict:** ☑ **PASS** *(illustrative)* — Install → Welcome → Sign in with Apple → Home. On a real walk, confirm the Apple sheet returns to Home without an `sign-in-with-apple-error` toast.

---

### TC-11 — Connect to the VPN

- **Persona:** The same user wanting protected browsing on a chosen region.
- **Goal (user's words):** *"As a user, I want to pick a region and connect the VPN, approving the one-time iOS permission."*
- **Surface:** iOS app, signed in from TC-10.
- **Preconditions:** Signed in; VPN configuration not yet added (so the OS permission dialog will appear on first connect).

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | Home | Tap **Connect** (`testID: connect-button`) | iOS shows **"iogrid Would Like to Add VPN Configurations"** | ✅ | [📷 tc11-1-vpn-permission](evidence-mobile/tc11-1-vpn-permission.png) |
| 2 | iOS VPN dialog | Tap **Allow**, authenticate if prompted | The status flips to **Connected** (`testID: connection-status`); the **egress IP** and **city** appear | ✅ | [📷 tc11-2-connected](evidence-mobile/tc11-2-connected.png) |
| 3 | Home (Connected) | Tap the region row → **Regions** | The **Regions** list with **Best (auto)** (`testID: region-best-auto`) + region cards; a search field | ❌ | [📷 tc11-3-regions](evidence-mobile/tc11-3-regions.png) |

- **Journey verdict:** ☒ **FAIL** *(illustrative example of a defect row)* — Steps 1–2 (permission + connect) are the core path. Step 3 illustrates how a real defect is recorded: e.g. *"the Regions list rendered but tapping a region card did not switch the active tunnel — the connected city stayed unchanged."* On a real walk, if that happens, file a P2 and link it here; if it works, mark ✅. **No real defect is claimed by this demonstration.**

---

### TC-12 — Top up balance to upgrade past the free quota

- **Persona:** A user who hit the 2 GB free quota and wants more, paying with Ping.
- **Goal (user's words):** *"As a user near my data cap, I want to top up my balance so my VPN keeps working."*
- **Surface:** iOS app, signed in.
- **Preconditions:** Signed in; a Ping wallet available on the device for the payment hand-off.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | Home | Tap **Top up** on the wallet card (`testID: wallet-card-topup`) | The **Top up** screen with preset amounts + a custom-amount field (`testID: topup-custom-input`) | ✅ | [📷 tc12-1-wallet-topup](evidence-mobile/tc12-1-wallet-topup.png) |
| 2 | Top up | Enter an amount, tap **Continue** (`testID: topup-continue`) | The app hands off to **Ping** via a Universal Link (`https://ping.cash/approve?…`) — the Ping approve screen, not a custom-scheme dialog | ✅ | [📷 tc12-2-topup-amount](evidence-mobile/tc12-2-topup-amount.png) |
| 3 | Ping approve | Approve the SPL delegate in Ping, return to iogrid | iogrid is re-opened via `iogrid://vpn/activated?ok=1&…`; the balance reflects the top-up | ⛔ | [📷 tc12-3-ping-approve](evidence-mobile/tc12-3-ping-approve.png) |

- **Journey verdict:** ☒ **BLOCKED** *(illustrative)* — Steps 1–2 are iogrid-side and walkable. Step 3 depends on the **external Ping app + a deployed `$GRID` mint** ([#665](https://github.com/iogrid/iogrid/issues/665) — Ping C-8 sig-verify ruling + mainnet mint); until those land, the round-trip can't complete on a real device, so the step is ⛔, not ❌. The Universal-Link hand-off itself (custom-scheme retired) is the realigned behavior from [#629](https://github.com/iogrid/iogrid/issues/629).

---

## Roll-up

| TC | Surface | Journey | Steps | Walked | ✅ | ❌ | ⛔ | Verdict |
|---|---|---|---|---|---|---|---|---|
| TC-10 | iOS app | First-run onboarding (Apple) | 4 | 4 | 4 | 0 | 0 | 🟢 PASS *(illustrative)* |
| TC-11 | iOS app | Connect to VPN | 3 | 3 | 2 | 1 | 0 | 🔴 FAIL *(illustrative defect row)* |
| TC-12 | iOS app | Top up via Ping | 3 | 3 | 2 | 0 | 1 | ⛔ BLOCKED *(external #665)* |
| | | **Total** | **10** | **10** | **8** | **1** | **1** | |

**Overall verdict:** ⬜ **NOT STARTED (demonstration)** — These marks demonstrate the recording format only; no live device was walked. A real iOS UAT (once an external TestFlight build is live, [#574](https://github.com/iogrid/iogrid/issues/574)) must re-capture every screenshot and re-judge each row.

---

## Defects found during this walk

> None real — this is a demonstration. The TC-11.3 ❌ is an **illustrative** example of how a defect row is written, not a claim that the app is broken.

| Defect | Step | What the user saw | Severity | Ticket |
|---|---|---|---|---|
| *(illustrative only — no real defect filed from this demonstration)* | TC-11.3 | *(example) region tap did not switch the active tunnel* | P2 | `#<n>` |

---

## Out of scope (handled by the dev team, NOT walked here)

Unit/integration/contract tests, CI pipelines, API/RPC/log verification, WireGuard tunnel internals, source reading. Capabilities with no on-screen surface have no UAT row.

---

_Filled from [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md) v1; sibling sample [`UAT-SAMPLE-web-provider-customer.md`](UAT-SAMPLE-web-provider-customer.md). Every row is one real tap a person could repeat on the device. Evidence here is placeholder pending a live device walk._
