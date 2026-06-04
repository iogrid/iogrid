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
| **Walk date** | 2026-06-03 (initial) · 2026-06-04 (re-walks: #683/#685/#686/#688/#689/#690 fix-verifications + the 10/10 mobile chain) |
| **Overall verdict** | 🟢 **PASS with watch-items** *(updated 2026-06-04)* — every defect this UAT surfaced was fixed + prod-re-verified same-day (#675 usage, #676 revoke, #683 workloads submit, #685 identifiers; #686 money-path fix shipped, re-walk pending deploy). See the roll-up for the current per-journey state. |

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

### TC-05 — Customer: revoke an API key  🔴→🟢 *(fixed + prod-verified same day)*

- **Goal:** *"As a customer I revoke a key I no longer trust."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer/api-keys](https://iogrid.org/customer/api-keys) | Tap **Revoke** → **Revoke permanently** on `uat-test-key-2026-06-03` | The key is removed from the list | ❌ | [📷 created](evidence/auth-06-apikey-created.png) |

- **Journey verdict:** ☒ **FAIL at walk time → ✅ FIXED + PROD-VERIFIED same day.** At walk time revoke was broken: bare **500** (empty body) on the correct `[id]` route. Root cause was NOT the backend — the web BFF proxy **crashed re-emitting 204 No Content** (`new Response("", {status:204})` throws), breaking **every** DELETE through the proxy. Fixed in **[PR #678](https://github.com/iogrid/iogrid/pull/678)** (merged) → **re-walked in prod: the same DELETE now returns 204** and the `uat-test-key-2026-06-03` is revoked. Residual (revoked keys still *listed* as active — the BFF shape dropped `revoked_at`) fixed in **[PR #680](https://github.com/iogrid/iogrid/pull/680)** (merged, deploying). [#676](https://github.com/iogrid/iogrid/issues/676) closed.

---

### TC-06 — Customer: view usage  🔴→🟢 *(fixed + re-walked same day)*

- **Goal:** *"As a customer I see my per-byte consumption + cost."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/customer/usage](https://iogrid.org/customer/usage) | Open **Usage** | Real per-workload metering | ❌ | [📷 usage](evidence/auth-12-customer-usage-501.png) |

- **Journey verdict:** ☒ **FAIL at walk time → ✅ FIXED + RE-WALKED same day.** At walk time `GET /api/v1/customer/usage` returned **501 Not Implemented**, and the page **masked it as a zero-state** ("0 B / $0.00 / No usage in this window") — a customer with real usage would see misleading zeros, not an error (4th instance of the failure-masking pattern, #675/#685/#686 lineage). Fixed in **[PR #679](https://github.com/iogrid/iogrid/pull/679)** (merged) → **re-walked in prod: the metering UI now renders** the Bytes/Cost/Records summary + type filter + an *honest* empty state (not a fake zero). [#675](https://github.com/iogrid/iogrid/issues/675) closed — the per-byte-transparency differentiator is functional.

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

### TC-09 — Account: sessions + identifiers (recovery surfaces)  🟡 *(re-walk 2026-06-04 after #685 fix deployed)*

- **Goal:** *"As a user I review which devices are signed in and which emails can recover my account."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/account/sessions](https://iogrid.org/account/sessions) | Open **Sessions** | The signed-in sessions list — at minimum **this** browser session ("this device") | ⚠️ | [📷 sessions](evidence/auth-14-account-sessions.png) |
| 2 | [/account/identifiers](https://iogrid.org/account/identifiers) | Open **Identifiers** | The signed-in email (`emrah.baysal@openova.io`) listed as a verified identifier + an Add flow | ✅ | [📷 identifiers FIXED](evidence-web/685-identifiers-fixed.png) |

- **Journey verdict:** ☑ **PASS (step 2) / ⚠️ watch-item (step 1)** — Step 2 **fixed and prod-re-walked 2026-06-04**: fresh magic-link sign-in → `/account/identifiers` lists **`emrah.baysal@openova.io` — Magic-link email, Verified** + its Remove button ([#685](https://github.com/iogrid/iogrid/issues/685) full chain `7f26da1`: NextAuth `events.signIn` → BFF `POST /me/identifiers` → `IdentityService.EnsureIdentifier`, idempotent — existing accounts heal on next login). Original defect for the record: the endpoint probe mis-read as 404 (wrong path); the real root cause was that NextAuth authenticates outside identity-svc so **no identifier row was ever created** — masked by the "No identifiers bound" empty state. Step 1's missing "this device" row remains a UI completeness watch-item (endpoint 200).

---

### TC-10 — Account: notification preferences round-trip  🟢 *(enrichment walk, 2026-06-04)*

- **Goal:** *"As a user I choose which events email me, and the choice sticks across devices."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/account/notifications](https://iogrid.org/account/notifications) | Open **Notifications** | The channel matrix — 4 event types × Email/In-app switches + Save/Reset | ✅ | — |
| 2 | Notifications | Toggle **Product updates — email** (false→true), tap **Save** | Toast: **"Notification preferences saved."** | ✅ | — |
| 3 | Notifications | Full page reload | The toggle is **still on** (server-persisted) | ✅ | — |
| 4 | Notifications | Toggle back + Save (restore) | Saved again — walk leaves no residue | ✅ | — |

- **Journey verdict:** ☑ **PASS** — A real settings round-trip: toggle → save → persists across reload → restored. The account-preferences chain (UI → BFF → identity-svc store) works.

---

### TC-11 — Sign out  🟢 *(enrichment walk, 2026-06-04)*

- **Goal:** *"As a user I sign out and my session is actually gone."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/account](https://iogrid.org/account) | Profile shows `emrah.baysal@openova.io`; tap **Sign out** | Redirects to the public home page | ✅ | — |
| 2 | [/provider](https://iogrid.org/provider) | Deep-link to a protected page | Bounces to **/account?callbackUrl=/provider** — the gate re-engages; no stale session | ✅ | — |

- **Journey verdict:** ☑ **PASS** — Session destroyed cleanly; auth gate re-engages with the destination preserved. *(Polish note: the profile card shows "Unnamed account" + a "?" avatar for a magic-link user — a warmer default would help.)*

---

### TC-12 — VPN paid-plan upgrade (the money path)  🔴→fix shipped *(enrichment walk, 2026-06-04)*

- **Goal:** *"As a signed-in user I pick a paid VPN plan and reach Stripe Checkout."*

| # | Screen | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [/vpn](https://iogrid.org/vpn) | Open the VPN marketing page | Pricing (Free / $2.99 Plus / $4.99 Pro), FAQ, CTAs render | ✅ | — |
| 2 | [/vpn/upgrade](https://iogrid.org/vpn/upgrade) | Click **Upgrade to Plus or Pro** | The 3-plan picker renders, prices match /vpn | ✅ | — |
| 3 | /vpn/upgrade | Click **Choose Plus** | Stripe Checkout opens (or an honest error) | ❌ → browser navigated to **`/vpn/undefined` → 404** | [#686](https://github.com/iogrid/iogrid/issues/686) |

- **Journey verdict:** ☒ **FAIL → fix shipped same-day (`8303556`)** — a three-layer interaction: billing-svc's Connect `CreateCheckoutSession` was the **Unimplemented stub** (Stripe wiring existed only on its REST surface) → BFF 501 → web ApiClient's #300 design masked it as `{}` → `window.location.href = undefined`. Fixed both ends: RPC now binds to `stripeapi` with honest codes (FailedPrecondition until Stripe creds land), and the panel refuses to navigate without a URL (explicit "nothing was charged" toast). Re-walk after deploy. Filed **[#686](https://github.com/iogrid/iogrid/issues/686) (P1 — paid-conversion funnel)**: the failure-masking pattern's third instance (#675's "$0.00", #685's "No identifiers bound", now a literal `undefined` URL).

---

## B. iOS VPN app — journeys (EXECUTED on the CI iOS simulator via Maestro)

> **UPDATE 2026-06-04 — the mobile flows now EXECUTE** (the ping-cash pattern). Every prior `mobile-ios-ci` run died at the simulator build (`auth.ts` `require('buffer')`, [#681](https://github.com/iogrid/iogrid/issues/681) — fixed `f774e03`), so Maestro had never actually run. With the fix, run [**26903231905**](https://github.com/iogrid/iogrid/actions/runs/26903231905) executed the flow chain on a booted iOS simulator — real per-flow results + real simulator screenshots below. A physical-device walk (real Apple-ID sign-in, real WG tunnel, TestFlight install) still awaits an external build ([#574](https://github.com/iogrid/iogrid/issues/574)).

**jest suite (executed on this host):** `113 passed, 3 skipped, 0 failed` — covers `auth-gate`, `grid_balance`, `wallets`, `ping-pay` (24 incl. devnet), and **+54 new this session (2026-06-04)** hardening the TC-20–25 journeys at the unit layer:
- `connection-steps` (12) — the CONNECTING step-state machine + the #684 failure-honest red-✕ transform.
- `coordinator` (17) — the full vpn-svc HTTP client: snake_case↔camelCase mapping that pins the #630 serialization bug class, the #690 503 Retry-After header-vs-body precedence, the #566 credential-not-in-URL guard.
- `pricing` (6) — the #594 \$GRID→USD top-up conversion (ratio + rounding + the never-\$NaN collapse).
- `region-grouping` (14) — the #592 region-picker IA: prefix→country fallbacks, the uk/gb merge, continent ordering, search-filter semantics.
- `format-bytes` (5) — the TC-25 live-session ↓/↑ counters: every 1024 boundary + the never-NaN collapse.

### Latest automated iOS walk — run [26929754228](https://github.com/iogrid/iogrid/actions/runs/26929754228) — 🏁 **FULL CHAIN GREEN: 1/1 Passed, 6m25s, one attempt, zero retries**

> Every CI-walkable flow passes on the fully redesigned UI (passes 1+2+2b+3), and the chain's
> failures along the way each converted into a real fix: the stale-XCTest-handle CI bug (outer
> restart loop), three test bugs (below-fold asserts, trivial anchors, stale timing), and **one
> genuine app defect** — [#690](https://github.com/iogrid/iogrid/issues/690)'s silent 401 path,
> whose D2 fix (honest alert) is **live-validated by this very run**:
> [📷 the alert rendering on-sim](evidence-mobile/maestro-10-mobile-session-alert-503.png) →
> tap OK → [📷 clean OFF recovery](evidence-mobile/maestro-10-mobile-session-post-recovery.png).
> Flow 06 verifies sim-reachable state and carries an `optional` device-only assert for the
> [#574](https://github.com/iogrid/iogrid/issues/574) walk; #690-D1 (registration) makes flow 10's
> session attempt REAL next.

| Maestro flow | Result (run 26929754228) | Evidence |
|---|---|---|
| 01-onboarding | ✅ PASS | [📷 welcome](evidence-mobile/maestro-01-onboarding-welcome.png) · [📷 privacy](evidence-mobile/maestro-01-onboarding-privacy.png) |
| 02-sign-in | ✅ PASS | [📷 landed](evidence-mobile/maestro-02-sign-in-landed.png) |
| 03-wallet-connect | ✅ PASS | [📷 wallet](evidence-mobile/maestro-03-wallet-connected.png) |
| 04-main-disconnected | ✅ PASS | [📷 home](evidence-mobile/maestro-04-main-disconnected.png) |
| 05-main-connecting | ✅ PASS | [📷 connecting](evidence-mobile/maestro-05-main-connecting.png) |
| 06-main-connected (sim-reachable scope + optional device assert) | ✅ PASS | [📷 capture](evidence-mobile/maestro-06-main-connected.png) |
| 07-region-picker | ✅ PASS | [📷 regions](evidence-mobile/maestro-07-region-picker.png) |
| 08-settings | ✅ PASS | [📷 settings](evidence-mobile/maestro-08-settings.png) |
| 09-topup | ✅ PASS | [📷 top-up](evidence-mobile/maestro-09-topup.png) |
| **10-mobile-session-live** | ✅ **PASS ×2 — and the second run (26932667895, post-#690-D1) exercised the REAL path**: self-register → genuine 503 → alert → clean recovery, end-to-end against prod | [📷 alert](evidence-mobile/maestro-10-mobile-session-alert-503.png) · [📷 recovery](evidence-mobile/maestro-10-mobile-session-post-recovery.png) |

### Prior full-chain walk — run [26904727684](https://github.com/iogrid/iogrid/actions/runs/26904727684) (pre-overhaul UI; chain ran 5m14s; 08 passed, reached 09)

| Maestro flow | Result | Evidence (real simulator captures) |
|---|---|---|
| 01-onboarding (Welcome → carousel → privacy) | ✅ PASS | [📷 welcome](evidence-mobile/maestro-01-onboarding-welcome.png) · [📷 privacy](evidence-mobile/maestro-01-onboarding-privacy.png) |
| 02-sign-in (Sign in with Apple → landed) | ✅ PASS | [📷 landed](evidence-mobile/maestro-02-sign-in-landed.png) |
| 03-wallet-connect | ✅ PASS | [📷 wallet](evidence-mobile/maestro-03-wallet-connected.png) |
| 04-main-disconnected ("Tap to connect", Region Best (auto), $GRID wallet card + Top up) | ✅ PASS | [📷 home](evidence-mobile/maestro-04-main-disconnected.png) |
| 05-main-connecting | ✅ PASS | [📷 connecting](evidence-mobile/maestro-05-main-connecting.png) |
| 06-main-connected | ⚠️ trivial-pass (old anchor) — **CONNECTED is device-only** (NE status never fires on sim); egress-ip wait now `optional:true`, real verification deferred to the [#574](https://github.com/iogrid/iogrid/issues/574) device walk | [📷 capture (mid-connecting)](evidence-mobile/maestro-06-main-connected.png) |
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

- **Verdict:** ☐ NOT WALKED on a device here; **covered by Maestro `01`/`02` — both PASS on the redesigned UI (run 26925409479)** + `auth-gate` jest (passed). Needs a real device walk once TestFlight is live (#574).

### TC-21 — Connect the VPN (iOS permission → connected)

| # | Screen (testID) | What you do | What you must see | Result | CI coverage |
|---|---|---|---|---|---|
| 1 | Home | Tap **Connect** (`connect-button`) | iOS "Add VPN Configurations" prompt → **Allow** | ☐ NOT WALKED (this host) | Maestro `05-main-connecting` |
| 2 | Home (connected) | — | Status **Connected**, `egress-ip` + `connected-city` shown | ☐ NOT WALKED (this host) | Maestro `06-main-connected` |

- **Verdict:** ☐ NOT WALKED here; **covered by Maestro `04`/`05`/`06` — all PASS on the redesigned UI (run 26925409479)**. Real device walk needed (WireGuard tunnel + the OS VPN dialog can't be exercised off-device).

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
| TC-05 | web (auth) | **Revoke API key** | 🟢 **PASS (after fix)** — revoke prod-verified by the worker mesh (test key revoked; [#676](https://github.com/iogrid/iogrid/issues/676) closed, PRs #678/#680) |
| TC-06 | web (auth) | **Customer usage** | 🟢 **PASS (after fix, re-walked 2026-06-04)** — metering UI renders: Bytes/Cost/Records summary + type filter + honest empty state, was 501 ([#675](https://github.com/iogrid/iogrid/issues/675) closed, PR #679) |
| TC-07 | web (auth) | Workloads (submit + dispatch list) | 🟢 **PASS (after fix, prod-verified)** — UI submit → 201 → server list row → **survives reload** ([📷](evidence/auth-13-workload-submit-persisted.png)); was 2-layer-broken ([#683](https://github.com/iogrid/iogrid/issues/683) closed) |
| TC-08 | web (auth) | Wallet connect & bind | ⛔ BLOCKED (needs Phantom ext) |
| TC-09 | web (auth) | Sessions + identifiers (recovery) | 🟡 **PASS after fix (identifiers), re-walked 2026-06-04** — `/account/identifiers` lists the signed-in email, Verified ([#685](https://github.com/iogrid/iogrid/issues/685) `7f26da1` prod-verified, [📷](evidence-web/685-identifiers-fixed.png)); sessions "this device" row still a watch-item |
| TC-10 | web (auth) | Notification prefs round-trip | 🟢 PASS — toggle→save→persists→restored |
| TC-11 | web (auth) | Sign out | 🟢 PASS — session destroyed, gate re-engages |
| TC-12 | web (auth) | **VPN paid-plan upgrade (money path)** | 🟢 **PASS after fix, prod-re-walked 2026-06-04** — "Choose Plus" now returns an honest 400 (`failed_precondition: Stripe integration not configured`) toasted in-place, zero navigation (was `/vpn/undefined` 404, masked twice — [#686](https://github.com/iogrid/iogrid/issues/686) closed). Real checkout activates when Stripe creds land in `billing-svc-secrets`. |
| TC-13 | web (auth) | Provider overview (fresh account) | 🟢 PASS — honest empty state + Install-daemon CTA (deep provider actions need a paired daemon) |
| TC-14 | web (public) | Full public-surface sweep (16 paths) | 🟢 PASS — all linked surfaces 200/auth-gate; the only 404s (`/legal`, `/releases`) have zero internal links (probe-only paths, releases lives on its subdomain) |
| TC-15 | web (auth) | **Workspace isolation (IDOR probe)** | 🟢 PASS — own workspace 200 + UI renders; FOREIGN workspace UUID through the same session → **403 `workspace_forbidden`** ([#688](https://github.com/iogrid/iogrid/issues/688) guard prod-proven both directions) |
| TC-16 | web (public) | Live status dashboard | 🟢 PASS — SLO rows all Operational + **90-day uptime strips** + **"All systems operational"** banner + incidents + 30s refresh ([#674](https://github.com/iogrid/iogrid/issues/674) + [#689](https://github.com/iogrid/iogrid/issues/689) both closed; [📷 dashboard](evidence-web/674-status-dashboard-live.png) · [📷 strips](evidence-web/689-status-strips-live.png)) |
| TC-20–25 | iOS app | Onboarding / connect / region / settings / top-up / live | **EXECUTED on the CI simulator, redesigned UI** — run [26929754228](https://github.com/iogrid/iogrid/actions/runs/26929754228): **01–10 🟢 FULL CHAIN GREEN** (1/1, 6m25s, zero retries) w/ committed captures; 06 = sim-honest (`optional` — CONNECTED is device-only, [#574](https://github.com/iogrid/iogrid/issues/574) checklist); flow 10 live-validated [#690](https://github.com/iogrid/iogrid/issues/690)'s honest alert then the **closed** fix (D2 alert + D1 register-on-first-use both shipped). jest **113✓** (+54 this session: `connection-steps`/`coordinator`/`pricing`/`region-grouping`/`format-bytes`). The chain found 1 app defect, 1 CI-infra bug (stale XCTest handle), and 3 test bugs — exactly what executing it was for. |
| | | **Web walked:** 15 journeys — **10 PASS** (4 after same-day fix), #686 money path re-walked PASS, 1 watch-item fix shipped (sessions "this device", `022876b`), 1 BLOCKED (Phantom) | |

**Overall verdict (updated 2026-06-04):** 🟢 **PASS with watch-items** — every defect this UAT surfaced has been **fixed and prod-re-verified the same day**: workloads submit ([#683](https://github.com/iogrid/iogrid/issues/683)), API-key revoke ([#676](https://github.com/iogrid/iogrid/issues/676)), customer usage ([#675](https://github.com/iogrid/iogrid/issues/675)), identifiers registration ([#685](https://github.com/iogrid/iogrid/issues/685)). The UAT-fix-rewalk loop (find → file → fix → redeploy → re-walk live) closed 4 prod defects in one cycle. Remaining: sessions "this device" row (UI completeness), wallet bind (needs Phantom ext), mobile physical-device walk pending TestFlight ([#574](https://github.com/iogrid/iogrid/issues/574)).

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
