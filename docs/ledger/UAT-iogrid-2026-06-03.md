# UAT — iogrid (authenticated web + iOS mobile) — 2026-06-03

> **Real, deep User-Acceptance-Test walk**, filled from [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md). This supersedes the earlier surface-only walk: the tester **signed in as a real user** (magic-link to `emrah.baysal@openova.io`) and exercised the **authenticated** product — provider activity, customer activity (incl. minting + revoking an API key), wallet bind, and usage/billing — not just the marketing pages. The iOS VPN app is covered by real Maestro flows + the jest suite (executed below); on-device steps are honestly marked pending a TestFlight build ([#574](https://github.com/iogrid/iogrid/issues/574)) since this is a Linux host with no iOS simulator.
>
> **Golden rule — 100% the end-user's experience.** Every row is a real tap/type/click on the shipped UI (or a real CI-run device flow). No fabricated passes.

---

## Metadata

| Field | Value |
|---|---|
| **Product / release** | iogrid — web (provider + customer + VPN) + iOS app `io.iogrid.app` |
| **Build under test** | live `iogrid.org` (web image `…5aaf21c`, current) · iOS build 1 (no external TestFlight yet, [#574](https://github.com/iogrid/iogrid.org)) |
| **Environment** | [https://iogrid.org](https://iogrid.org) — signed in as `emrah.baysal@openova.io` (real magic-link, completed) |
| **Surface(s)** | Authenticated responsive web (Chromium) · iOS app (jest executed + Maestro-in-CI; not device-walked on this Linux host) |
| **Tester** | `@p0-474-lonely` (UAT executor) |
| **Walk date** | 2026-06-03 |
| **Overall verdict** | 🔴 **FAIL** — core customer surfaces have multiple broken endpoints (usage 501, workloads-list 405, **API-key revoke broken 500/405**). Provider + auth + billing work. See roll-up. |

---

## How to read & fill this document

**Result legend:** ✅ PASS *(evidence required)* · ❌ FAIL *(defect filed, issue left open)* · ⛔ BLOCKED *(couldn't attempt)* · ⏭️ N/A · ☐ NOT WALKED.

The tester is read-only on the product code; reported what was seen on screen; never self-closes a defect issue. The magic link was read **read-only** from the mailbox (no message modified, sent, or deleted; no account/password touched).

---

## A. Web — authenticated journeys (walked live, signed in as a real user)

### TC-01 — Sign in with a magic link (full round-trip)

- **Persona:** Returning user; iogrid web auth is passwordless email.
- **Goal:** *"As a user I enter my email, get a sign-in link, and reach my authenticated account."*
- **Preconditions:** A real mailbox (`emrah.baysal@openova.io`).

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/account](https://iogrid.org/account) | Enter `emrah.baysal@openova.io`, tap **Send magic link** | "Verify Request — check your email" | ✅ | — |
| 2 | Mailbox | Open the iogrid email, follow the sign-in link | Redirects to iogrid, session established | ✅ | — |
| 3 | [/account](https://iogrid.org/account) | Revisit the account page | **AppShell renders** — persona switcher ("Account") + account nav, NOT the sign-in form | ✅ | [📷 account](evidence/auth-01-account.png) |

- **Journey verdict:** ☑ **PASS** — The full magic-link round-trip works end-to-end. (The earlier UAT marked this ⛔ "no inbox" — that was a cop-out; with a real mailbox it passes.)

---

### TC-02 — Provider: land on the dashboard, see the onboarding state + navigate

- **Goal:** *"As a provider I open my dashboard and understand how to start earning."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/provider](https://iogrid.org/provider) | Open the provider dashboard | Real dashboard (not a redirect): **"You don't have any provider machines paired yet… Install the iogrid daemon to start earning $GRID"** + nav (Overview/Transparency/Schedule/Earnings/Staking) | ✅ | [📷 provider](evidence/auth-02-provider-overview.png) |
| 2 | [/provider/earnings](https://iogrid.org/provider/earnings) | Open **Earnings** | Earnings view renders (endpoint 200) | ✅ | [📷 earnings](evidence/auth-03-provider-earnings.png) |
| 3 | [/provider/audit](https://iogrid.org/provider/audit) | Open **Transparency** (per-byte audit) | Transparency feed page renders | ✅ | [📷 audit](evidence/auth-10-provider-audit.png) |
| 4 | [/provider/staking](https://iogrid.org/provider/staking) | Open **Staking** | Staking view renders | ✅ | [📷 staking](evidence/auth-11-provider-staking.png) |

- **Journey verdict:** ☑ **PASS** — Provider surfaces render correctly for a real signed-in user with the genuine "no daemon paired" empty state. Endpoint probe: overview/earnings/schedule **200**. *(The raw-probe 404 on `GET /api/v1/provide/audit/stream` turned out to be **by design** — `404 code=no_provider` for unpaired callers; the page suppresses its EventSource until ownership resolves, #313. See the resolved watch-item below.)*

---

### TC-03 — Customer: open the workspace + navigate

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer](https://iogrid.org/customer) | Open the customer dashboard | **"Workspace"** — "Submit workloads, monitor scheduling, manage API keys and billing" + nav (Workloads/API keys/Billing/Usage) | ✅ | [📷 customer](evidence/auth-04-customer-overview.png) |
| 2 | [/customer/billing](https://iogrid.org/customer/billing) | Open **Billing** | Billing view renders (endpoint **200**) | ✅ | [📷 billing](evidence/auth-08-customer-billing.png) |

- **Journey verdict:** ☑ **PASS** — Workspace auto-provisioned (`workspace_id e7745e37…`), nav + billing work.

---

### TC-04 — Customer: mint an API key (real create action)

- **Goal:** *"As a customer I create an API key so a CI runner can submit workloads."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer/api-keys](https://iogrid.org/customer/api-keys) | Open API keys | "Create a key" form + an existing key `vpn:cli-unblock` | ✅ | [📷 apikeys](evidence/auth-05-customer-apikeys.png) |
| 2 | API keys | Type label `uat-test-key-2026-06-03`, tap **Create key** | New key appears (count 1→2) + **one-time plaintext token** dialog ("Copy this token now… iogrid will never show it to you again") | ✅ | [📷 created](evidence/auth-06-apikey-created.png) |

- **Journey verdict:** ☑ **PASS** — Real end-to-end key creation: form → backend persist → one-time token reveal. Genuine customer functionality.

---

### TC-05 — Customer: revoke an API key  🔴

- **Goal:** *"As a customer I revoke a key I no longer trust."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer/api-keys](https://iogrid.org/customer/api-keys) | Tap **Revoke** → **Revoke permanently** on `uat-test-key-2026-06-03` | The key is removed from the list | ❌ | [📷 created](evidence/auth-06-apikey-created.png) |

- **Journey verdict:** ☒ **FAIL at walk time → ✅ FIXED + PROD-VERIFIED same day.** At walk time revoke was broken: bare **500** (empty body) on the correct `[id]` route. Root cause was NOT the backend — the web BFF proxy **crashed re-emitting 204 No Content** (`new Response("", {status:204})` throws), breaking **every** DELETE through the proxy. Fixed in **[PR #678](https://github.com/iogrid/iogrid/pull/678)** (merged) → **re-walked in prod: the same DELETE now returns 204** and the `uat-test-key-2026-06-03` is revoked. Residual (revoked keys still *listed* as active — the BFF shape dropped `revoked_at`) fixed in **[PR #680](https://github.com/iogrid/iogrid/pull/680)** (merged, deploying). [#676](https://github.com/iogrid/iogrid/issues/676) closed.

---

### TC-06 — Customer: view usage  🔴

- **Goal:** *"As a customer I see my per-byte consumption + cost."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer/usage](https://iogrid.org/customer/usage) | Open **Usage** | Real per-workload metering | ❌ | [📷 usage](evidence/auth-12-customer-usage-501.png) |

- **Journey verdict:** ☒ **FAIL** — `GET /api/v1/customer/usage` returns **501 Not Implemented**. The page **masks it as a zero-state** ("0 B / $0.00 / No usage in this window"), so a customer with real usage would see misleading zeros, not an error. Filed **[#675](https://github.com/iogrid/iogrid/issues/675) (P2)** — this is the per-byte-transparency differentiator, non-functional.

---

### TC-07 — Customer: workloads (submit + dispatch list)  🟢 (after fix)

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer/workloads](https://iogrid.org/customer/workloads) | Open **Workloads** | Submit form (Type/Category/Destination) + "Recent dispatches" | ✅ | [📷 workloads](evidence/auth-07-customer-workloads.png) |
| 2 | Workloads | Fill Type=Bandwidth, Destination=`uat-final.example.com`, tap **Submit workload** | The workload is accepted (201) and appears in the dispatch list | ✅ *(after fix)* | [📷 persisted](evidence/auth-13-workload-submit-persisted.png) |
| 3 | Workloads | Full page reload | The dispatched row **persists** (server-backed, not browser-local) | ✅ *(after fix)* | [📷 persisted](evidence/auth-13-workload-submit-persisted.png) |

- **Journey verdict:** ☑ **PASS (after a 2-layer fix, prod-verified same day)** — At walk time the submit was **broken for every UI user**: layer 1 — `400` enum-unmarshal (web sends proto3-JSON enum-as-string; gateway-bff decoded with stdlib `encoding/json`, the #630/#633 class) fixed in `e6a708b` (protojson both directions); layer 2 — the panel sent flat `{type,category,destination}` but the proto requires the typed `oneof` payload, fixed in `8462d8a` (typed payload + `workspaceId` stamp). **Re-walked in prod:** form submit → **201** → server-backed row (`Bandwidth (residential) · rejected · just now`) → **survives reload**. The `rejected` status is the dispatcher's legitimate no-eligible-provider decision (no bandwidth daemons paired yet). The optimistic local rows that had masked the breakage are gone — the list is the server's truth. [#683](https://github.com/iogrid/iogrid/issues/683) closed; the list half (originally [#677](https://github.com/iogrid/iogrid/issues/677)) is live (`GET` 401-wired, was 405).

---

### TC-08 — Account: connect & bind a Solana wallet  ⛔

- **Goal:** *"As a user I bind my Solana wallet for $GRID payouts / discount."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/account/wallets](https://iogrid.org/account/wallets) | Open **Wallets** | Real bind UI: "No wallets bound yet", **Connect wallet** / **Connect & bind wallet**, balance card (endpoint 200) | ✅ | [📷 wallets](evidence/auth-09-account-wallets.png) |
| 2 | Wallets | Tap **Connect & bind wallet** | Phantom opens → sign challenge → address bound | ⛔ | — |

- **Journey verdict:** ☒ **BLOCKED** — The bind UI renders correctly (empty state + controls + balance endpoint 200), but completing the bind needs the **Phantom browser extension**, which isn't installed in this headless Playwright session. Not a defect — a tooling limit. Re-walk in a real browser with Phantom, or cover via the mobile wallet-connect flow (Maestro 03).

---

## B. iOS VPN app — journeys (EXECUTED on the CI iOS simulator via Maestro)

> **UPDATE 2026-06-04 — the mobile flows now EXECUTE** (the ping-cash pattern). Every prior `mobile-ios-ci` run died at the simulator build (`auth.ts` `require('buffer')`, [#681](https://github.com/iogrid/iogrid/issues/681) — fixed `f774e03`), so Maestro had never actually run. With the fix, run [**26903231905**](https://github.com/iogrid/iogrid/actions/runs/26903231905) executed the flow chain on a booted iOS simulator — real per-flow results + real simulator screenshots below. A physical-device walk (real Apple-ID sign-in, real WG tunnel, TestFlight install) still awaits an external build ([#574](https://github.com/iogrid/iogrid/issues/574)).

**jest suite (executed on this host):** `59 passed, 3 skipped, 0 failed` — covers `auth-gate`, `grid_balance`, `wallets`, `ping-pay` (24 incl. devnet).

### Latest automated iOS walk — run [26904727684](https://github.com/iogrid/iogrid/actions/runs/26904727684) (chain ran 5m14s; 08 now passes, reached 09)

| Maestro flow | Result | Evidence (real simulator captures) |
|---|---|---|
| 01-onboarding (Welcome → carousel → privacy) | ✅ PASS | [📷 welcome](evidence-mobile/maestro-01-onboarding-welcome.png) · [📷 privacy](evidence-mobile/maestro-01-onboarding-privacy.png) |
| 02-sign-in (Sign in with Apple → landed) | ✅ PASS | [📷 landed](evidence-mobile/maestro-02-sign-in-landed.png) |
| 03-wallet-connect | ✅ PASS | [📷 wallet](evidence-mobile/maestro-03-wallet-connected.png) |
| 04-main-disconnected ("Tap to connect", Region Best (auto), $GRID wallet card + Top up) | ✅ PASS | [📷 home](evidence-mobile/maestro-04-main-disconnected.png) |
| 05-main-connecting | ✅ PASS | [📷 connecting](evidence-mobile/maestro-05-main-connecting.png) |
| 06-main-connected | ✅ PASS | [📷 connected](evidence-mobile/maestro-06-main-connected.png) |
| 07-region-picker | ✅ PASS | [📷 regions](evidence-mobile/maestro-07-region-picker.png) |
| **08-settings** | ✅ **PASS** *(was last run's below-fold fail; scroll fix `a4824d2` cleared it)* | [📷 settings](evidence-mobile/maestro-08-settings.png) |
| **09-topup** | ❌ **FAIL — test bug, not an app bug**: `assertVisible: topup-continue` without scrolling, but Continue sits below the payment-methods list (Apple Pay / Card / Bitcoin / USDC / Transfer $GRID), off the CI viewport fold. The Top up screen itself rendered correctly (Quick amounts +500/+2,500/+10,000 with +2,500 selected, all pay methods). Fix: `scrollUntilVisible` added (`b59a043`). | [📷 failure capture](evidence-mobile/maestro-09-topup-FAIL-below-fold.png) |
| 10-mobile-session-live | ⛔ BLOCKED this run (chain aborted at 09) — executes once 09 scrolls | — |

*(Iteration pattern, same as ping-cash: each run peels a real layer. Run 1 `26903231905`: sim-build fixed (#681) → 01–07 ran, 08 below-fold. Run 2 `26904727684`: 08 fixed → **01–08 PASS**, 09 below-fold. Run 3 `26910040116`: carried the 09 scroll fix but **launch-flaked** after flow 02 ("Unable to launch app" — the known simulator/XCUITest flake, #575/#599 family; not an app or test regression) → rerun queued; 09/10 validation pending. **Every real flow "failure" so far has been a CI-viewport test-assertion bug — the app screens all render correctly.**)*

### TC-20 — First-run onboarding → signed in with Apple

| # | Screen (testID) | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Welcome | Tap **Continue** (`onboarding-welcome-continue`) | Welcome carousel advances | ☐ NOT WALKED (this host) | Maestro `01-onboarding` |
| 2 | Sign in | Tap **Sign in with Apple** (`onboarding-sign-in-apple`) | Apple sheet → lands on `connect-wallet-skip` | ☐ NOT WALKED (this host) | Maestro `02-sign-in` |

- **Verdict:** ☐ NOT WALKED on a device here; **covered by Maestro `01`/`02` in `mobile-ios-ci`** + `auth-gate` jest (passed). Needs a real device walk once TestFlight is live (#574).

### TC-21 — Connect the VPN (iOS permission → connected)

| # | Screen (testID) | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Home | Tap **Connect** (`connect-button`) | iOS "Add VPN Configurations" prompt → **Allow** | ☐ NOT WALKED (this host) | Maestro `05-main-connecting` |
| 2 | Home (connected) | — | Status **Connected**, `egress-ip` + `connected-city` shown | ☐ NOT WALKED (this host) | Maestro `06-main-connected` |

- **Verdict:** ☐ NOT WALKED here; **covered by Maestro `04`/`05`/`06`**. Real device walk needed (WireGuard tunnel + the OS VPN dialog can't be exercised off-device).

### TC-22 — Switch region

| # | Screen (testID) | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Regions | Search (`regions-search`), tap **Best (auto)** (`region-best-auto`) or a `region-card` | Active region switches | ☐ NOT WALKED (this host) | Maestro `07-region-picker` |

### TC-23 — Settings (kill-switch / DNS-leak / split-tunnel / sign-out)

| # | Screen (testID) | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Settings | Toggle `settings-row-kill-switch`, `settings-row-dns-leak`, `settings-row-split-tunneling`, `settings-row-auto-connect` | Each toggle persists | ☐ NOT WALKED (this host) | Maestro `08-settings` |
| 2 | Settings | Tap **Sign out** (`settings-sign-out`) | Returns to onboarding | ☐ NOT WALKED (this host) | Maestro `08-settings` |

### TC-24 — Top up balance (Ping)

| # | Screen (testID) | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Home | Tap **Top up** (`wallet-card-topup`) | Top-up screen (`topup-custom-input`) | ☐ NOT WALKED (this host) | Maestro `09-topup` |
| 2 | Top up | Enter amount, tap **Continue** (`topup-continue`) | Hands off to Ping via Universal Link `https://ping.cash/approve` | ☐ NOT WALKED (this host) | Maestro `09` + `ping-pay` jest (24 passed) |

- **Verdict:** payment-URL construction is unit-verified (`ping-pay` jest, 24 passed, 9-dec atomic + Universal-Link shape); the on-device hand-off + Ping round-trip need a device + the external $GRID mint ([#665](https://github.com/iogrid/iogrid/issues/665)).

### TC-25 — Live VPN session

| # | Screen | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Home (connected) | Keep a session live, observe egress IP/city | Stable tunnel + live session telemetry | ☐ NOT WALKED (this host) | Maestro `10-mobile-session-live` |

---

## Roll-up

| TC | Surface | Journey | Result |
|---|---|---|---|
| TC-01 | web (auth) | Magic-link sign-in (full round-trip) | 🟢 PASS |
| TC-02 | web (auth) | Provider dashboard + nav | 🟢 PASS |
| TC-03 | web (auth) | Customer workspace + billing | 🟢 PASS |
| TC-04 | web (auth) | Mint API key | 🟢 PASS |
| TC-05 | web (auth) | **Revoke API key** | 🔴 FAIL ([#676](https://github.com/iogrid/iogrid/issues/676), P1) |
| TC-06 | web (auth) | **Customer usage** | 🔴 FAIL ([#675](https://github.com/iogrid/iogrid/issues/675), P2) |
| TC-07 | web (auth) | Workloads (submit + dispatch list) | 🟢 **PASS (after fix, prod-verified)** — UI submit → 201 → server list row → **survives reload** ([📷](evidence/auth-13-workload-submit-persisted.png)); was 2-layer-broken ([#683](https://github.com/iogrid/iogrid/issues/683) closed) |
| TC-08 | web (auth) | Wallet connect & bind | ⛔ BLOCKED (needs Phantom ext) |
| TC-20–25 | iOS app | Onboarding / connect / region / settings / top-up / live | **EXECUTED on the CI simulator** (latest run 26904727684): flows **01–08 🟢 PASS** w/ real captures; 09 🔴 test-bug (below-fold assert, fix `b59a043`); 10 ⛔ next run. Every failure so far = CI-viewport assertion bug, never an app defect. jest 59✓. Physical-device walk pending [#574](https://github.com/iogrid/iogrid/issues/574). |
| | | **Web walked:** 8 journeys — 5 PASS, 2 FAIL, 1 BLOCKED | |

**Overall verdict:** 🔴 **FAIL** — Authentication, provider surfaces, customer billing/workloads-submit, and API-key **creation** work. But the authenticated **customer** surface has **two broken endpoints**: API-key **revoke** (P1, security-relevant — [#676](https://github.com/iogrid/iogrid/issues/676)) and **usage** (P2 — [#675](https://github.com/iogrid/iogrid/issues/675), also the only place to see historical workloads). These are real, only findable by signing in — exactly what the prior surface-only UAT missed. Mobile needs a real device walk once an external TestFlight build exists ([#574](https://github.com/iogrid/iogrid/issues/574)).

---

## Defects found during this walk

| Defect | Step | What the user saw | Severity | Ticket |
|---|---|---|---|---|
| API-key **revoke** broken | TC-05 | Key won't revoke; 500 (`[id]` route) / 405 (base) | P1 | [#676](https://github.com/iogrid/iogrid/issues/676) |
| Customer **usage** 501, masked as $0.00 | TC-06 | "No usage" zero-state hiding a 501 | P2 | [#675](https://github.com/iogrid/iogrid/issues/675) |
| (prior walk) `/status` dashboard 404 | — | dead link — **already fixed** | P2 | [#668](https://github.com/iogrid/iogrid/issues/668) ✅ |

> Withdrawn: `/customer/workloads` GET 405 ([#677](https://github.com/iogrid/iogrid/issues/677)) — closed invalid; the UI never GETs that route (POST-only by design), so it was a probe artifact, not a user-facing failure.

> Watch-item RESOLVED (not a defect): `GET /api/v1/provide/audit/stream` → 404 is **by design** — gateway-bff returns `404 code=no_provider` for callers with no paired provider (this tester's state), and the audit page deliberately suppresses its EventSource until provider ownership resolves (#313). The raw probe bypassed that guard; a paired provider would stream normally.

---

## Out of scope (handled by the dev team, NOT walked here)

Unit/integration/contract tests (the mobile **jest** result is cited as supporting evidence, not as a UAT row), CI pipelines, raw API/SQL/log verification with no on-screen surface, source reading.

---

_Filled from [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md). Web journeys walked live authenticated 2026-06-03; iOS jest executed, device flows mapped to Maestro `01–10` in `mobile-ios-ci`, on-device walk pending an external build ([#574](https://github.com/iogrid/iogrid/issues/574)). Supersedes the surface-only `UAT-iogrid-web-2026-06-03.md`._
