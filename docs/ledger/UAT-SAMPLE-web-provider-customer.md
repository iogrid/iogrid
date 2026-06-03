# UAT — iogrid web (provider & customer) — 2026-06-03

> **Standard User-Acceptance-Test walk.** *(A **filled sample** of [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md) for the **web** surface. The screenshots in this file are **real captures from a live Playwright walk of `iogrid.org`** on 2026-06-03 — not placeholders.)*
>
> **Golden rule — this document is 100% the end-user's experience.** Every step is something a person does with their thumb or mouse on the shipped UI. No terminal, no `kubectl`, no API calls, no code reading. A non-technical beta tester must be able to follow every row verbatim.

---

## Metadata

| Field | Value |
|---|---|
| **Product / release** | iogrid web (providers + customers + VPN marketing) |
| **Build under test** | live `iogrid.org` (image-reroll cron deploy, 2026-06-03) |
| **Environment** | [https://iogrid.org](https://iogrid.org) |
| **Surface(s)** | Responsive web (walked in Chromium, desktop viewport) |
| **Tester** | `@p0-474-lonely` (UAT executor) |
| **Walk date** | 2026-06-03 |
| **Overall verdict** | ⚠️ CONDITIONAL *(core journeys pass; magic-link completion is inbox-gated in this walk — see roll-up)* |

---

## How to read & fill this document

**Result legend:** ✅ PASS *(evidence required)* · ❌ FAIL *(file defect, leave issue open)* · ⛔ BLOCKED *(couldn't attempt)* · ⏭️ N/A · ☐ NOT WALKED.

Rules: no ✅ without a screenshot; walk in order; executor is read-only on the product; report the screen you saw, not the screen you expected; the executor never closes the issue.

---

## Test journeys

### TC-01 — Evaluate iogrid and download the provider daemon

- **Persona:** A home-PC owner who heard they can earn by sharing idle compute/bandwidth, browsing on desktop.
- **Goal (user's words):** *"As a prospective provider, I want to understand what iogrid offers and download the installer for my OS, so that I can start sharing my machine."*
- **Surface:** Responsive web (Chromium desktop).
- **Preconditions:** None — a fresh, signed-out visitor.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org](https://iogrid.org) | Open the landing page | The iogrid home page — hero "The mesh that shows you every byte" (browser tab reads "iogrid — Distributed compute mesh"), with **VPN** and **Become a provider** entry points | ✅ | [📷 tc01-1-home](evidence/tc01-1-home.png) |
| 2 | [iogrid.org/vpn](https://iogrid.org/vpn) | Open the **VPN** page | The VPN plans: **Free** (2 GB/mo), **Plus** ($2.99), **Pro** ($4.99) with **Get** buttons | ✅ | [📷 tc01-2-vpn-pricing](evidence/tc01-2-vpn-pricing.png) |
| 3 | [iogrid.org/install](https://iogrid.org/install) | Open the **Install** page | Per-OS **Download** options — **macOS** (`.pkg`), **Windows** (`.msi`), **Linux** (`.deb`) | ✅ | [📷 tc01-3-install](evidence/tc01-3-install.png) |

- **Journey verdict:** ☑ **PASS** — Landing → VPN pricing → per-OS installer page all rendered correctly for a signed-out visitor; download options present for all three desktop OSes.

---

### TC-02 — Sign in with a magic link

- **Persona:** A returning user (provider or customer) who wants into their account; iogrid uses passwordless email sign-in.
- **Goal (user's words):** *"As a user, I want to enter my email and get a sign-in link, so that I reach my account without a password."*
- **Surface:** Responsive web (Chromium desktop).
- **Preconditions:** An email address. *(This walk used a non-monitored address, so the final inbox step is blocked — see step 4.)*

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/account](https://iogrid.org/account) | Open the account page while signed out | A **"Sign in to iogrid"** card with an **Email** field and a **Send magic link** button; copy explains one account covers provider + customer. *(No Google button — correctly hidden until a real OAuth client is configured, [#653](https://github.com/iogrid/iogrid/issues/653).)* | ✅ | [📷 tc02-1-signin](evidence/tc02-1-signin.png) |
| 2 | Sign in to iogrid | Type an email into **Email**, tap **Send magic link** | The page advances to a **"Verify Request — check your email"** confirmation (no error toast) | ✅ | [📷 tc02-2-check-email](evidence/tc02-2-check-email.png) |
| 3 | Email inbox | Open the magic-link email, click **Sign in** | Lands authenticated on the account dashboard | ⛔ | — |

- **Journey verdict:** ☒ **BLOCKED** (not a fail) — The on-screen half works end-to-end: entering an email and tapping **Send magic link** correctly advances to the "check your email" verify-request screen. Step 3 is **blocked** because this walk used a non-monitored test address with no inbox access — the link could not be opened to complete sign-in. Re-walk with a monitored inbox to confirm the post-login dashboard. No defect: nothing errored on screen.

---

### TC-03 — Protected pages require sign-in (auth gate)

- **Persona:** A signed-out visitor who tries to deep-link straight into the provider dashboard.
- **Goal (user's words):** *"As a signed-out user, when I hit a protected page I should be sent to sign in, not shown someone's data."*
- **Surface:** Responsive web (Chromium desktop).
- **Preconditions:** Signed out.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/provider](https://iogrid.org/provider) | Navigate to the provider dashboard URL while signed out | The app redirects to **[/account?callbackUrl=/provider](https://iogrid.org/account?callbackUrl=%2Fprovider)** — the sign-in card, with the intended destination preserved | ✅ | [📷 tc03-1-provider-authgate](evidence/tc03-1-provider-authgate.png) |

- **Journey verdict:** ☑ **PASS** — Protected `/provider` correctly bounces a signed-out user to sign-in (with `callbackUrl` preserved so they return after login). No provider data leaked to an unauthenticated visitor.

---

## Roll-up

| TC | Surface | Journey | Steps | Walked | ✅ | ❌ | ⛔ | Verdict |
|---|---|---|---|---|---|---|---|---|
| TC-01 | web | Evaluate + download daemon | 3 | 3 | 3 | 0 | 0 | 🟢 PASS |
| TC-02 | web | Magic-link sign-in | 3 | 3 | 2 | 0 | 1 | ⛔ BLOCKED (inbox-gated) |
| TC-03 | web | Auth gate on protected page | 1 | 1 | 1 | 0 | 0 | 🟢 PASS |
| | | **Total** | **7** | **7** | **6** | **0** | **1** | |

**Overall verdict:** ⚠️ **CONDITIONAL** — The unauthenticated journeys (evaluate/download, auth-gating) are go-live-ready and the magic-link *request* works on screen. The only gap is the inbox-gated completion of sign-in (TC-02.3), which is a walk-setup limitation, not a product defect. Re-walk TC-02 with a monitored inbox to flip it 🟢. No P0/P1/P2 defects found.

---

## Defects found during this walk

| Defect | Step | What the user saw | Severity | Ticket |
|---|---|---|---|---|
| *(none)* | — | — | — | — |

> Note: the home page logs a cosmetic `favicon.ico 404` in the browser console. It has no on-screen surface a user acts on, so per the golden rule it is **not** a UAT row — flagged here only for the dev team.

---

## Out of scope (handled by the dev team, NOT walked here)

Unit/integration/contract tests, CI pipelines, API/`kubectl`/SQL/log verification, source reading. Capabilities with no on-screen surface have no UAT row.

---

_Filled from [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md) v1; sibling sample [`UAT-SAMPLE-mobile-vpn.md`](UAT-SAMPLE-mobile-vpn.md). Every row is one thumb/mouse action a real user could repeat._
