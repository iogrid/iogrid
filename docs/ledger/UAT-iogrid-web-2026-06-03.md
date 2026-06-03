# UAT — iogrid — 2026-06-03

> ⚠️ **SUPERSEDED by [`UAT-iogrid-2026-06-03.md`](UAT-iogrid-2026-06-03.md)** — that doc is the real *authenticated* walk (signed in, exercised provider/customer/wallet, found 3 real defects). This file was a surface-only walk of unauthenticated pages.

> **Standard User-Acceptance-Test walk** of the real iogrid product, filled from [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md). This is a **real walk** — the web journeys (TC-01…TC-07) were executed live against `iogrid.org` with Playwright and every ✅ row has a real screenshot. The mobile journeys (TC-10…TC-12) are **NOT WALKED** — there is no live external TestFlight build to install yet ([#574](https://github.com/iogrid/iogrid/issues/574)).
>
> **Golden rule — this document is 100% the end-user's experience.** Every step is something a person does with their thumb or mouse on the shipped UI. No terminal, no `kubectl`, no API calls, no code reading.

---

## Metadata

| Field | Value |
|---|---|
| **Product / release** | iogrid — web (providers, customers, VPN) + iOS app |
| **Build under test** | live `iogrid.org` (image-reroll cron deploy, 2026-06-03) |
| **Environment** | [https://iogrid.org](https://iogrid.org) |
| **Surface(s)** | Responsive web (walked in Chromium, desktop viewport) · iOS app `io.iogrid.app` (not walked — no build) |
| **Tester** | `@p0-474-lonely` (UAT executor) |
| **Walk date** | 2026-06-03 |
| **Overall verdict** | ⚠️ CONDITIONAL *(1 P2 defect [#668](https://github.com/iogrid/iogrid/issues/668); sign-in completion inbox-gated; mobile not walked — see roll-up)* |

---

## How to read & fill this document

**Result legend:** ✅ PASS *(evidence required)* · ❌ FAIL *(file defect, leave issue open)* · ⛔ BLOCKED *(couldn't attempt)* · ⏭️ N/A · ☐ NOT WALKED.

Rules: no ✅ without a screenshot; walk in order; executor is read-only on the product; report the screen you saw, not the screen you expected; the executor never closes the issue.

---

## Test journeys

### TC-01 — Prospective provider evaluates iogrid and downloads the daemon

- **Persona:** A home-PC / Mac owner who heard they can earn from idle compute + bandwidth.
- **Goal:** *"As a prospective provider, I want to understand iogrid and download the installer for my OS, so that I can start sharing my machine."*
- **Surface:** Responsive web (Chromium desktop). **Preconditions:** none (signed-out visitor).

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org](https://iogrid.org) | Open the landing page | iogrid home — hero "The mesh that shows you every byte" (tab title "iogrid — Distributed compute mesh"), with **VPN** + **Become a provider** entry points | ✅ | [📷 home](evidence/tc01-1-home.png) |
| 2 | [iogrid.org/install](https://iogrid.org/install) | Open the **Install** page | Per-OS **Download** options — **macOS** (`.pkg`), **Windows** (`.msi`), **Linux** (`.deb`) | ✅ | [📷 install](evidence/tc01-3-install.png) |

- **Journey verdict:** ☑ **PASS** — Landing → per-OS installer page rendered correctly for a signed-out visitor; all three desktop OS downloads present.

---

### TC-02 — Customer evaluates the four workload offerings

- **Persona:** An engineering buyer comparing iogrid against GitHub Actions / cloud GPU / residential-proxy vendors.
- **Goal:** *"As a customer, I want to see what iogrid offers and what it costs, so that I can decide if it beats my current vendor."*
- **Surface:** Responsive web. **Preconditions:** none.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/compute](https://iogrid.org/compute) | Open **Docker compute** | "Docker compute" priced **$0.018 / vCPU-hour** on home + Mac hardware | ✅ | [📷 compute](evidence/tc02-1-compute.png) |
| 2 | [iogrid.org/proxy](https://iogrid.org/proxy) | Open **Residential proxy** | "Residential proxy" at **$0.30–$0.60 / GB** with per-byte audit | ✅ | [📷 proxy](evidence/tc02-2-proxy.png) |
| 3 | [iogrid.org/gpu](https://iogrid.org/gpu) | Open **GPU inference** | "GPU inference" at **$0.20 / GPU-hour** on consumer + Apple Silicon | ✅ | [📷 gpu](evidence/tc02-3-gpu.png) |
| 4 | [iogrid.org/ios-build](https://iogrid.org/ios-build) | Open **iOS build CI** | "iOS build CI" at **$0.04 / Xcode-minute** on real Macs | ✅ | [📷 ios-build](evidence/tc02-4-ios-build.png) |

- **Journey verdict:** ☑ **PASS** — All four workload pages render with concrete, differentiated pricing (the "~50% under market" + per-byte-transparency positioning is visible). The first-class iOS-build workload is present and priced.

---

### TC-03 — Customer checks consolidated pricing

- **Persona:** The same buyer wanting one pricing view.
- **Goal:** *"As a customer, I want a single pricing page so I can compare all workloads at once."*
- **Surface:** Responsive web. **Preconditions:** none.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/pricing](https://iogrid.org/pricing) | Open the **Pricing** page | Bandwidth / compute / GPU / iOS-build pricing, positioned **30–60% under market** | ✅ | [📷 pricing](evidence/tc03-1-pricing.png) |

- **Journey verdict:** ☑ **PASS** — Consolidated pricing page renders with the cross-workload table and the under-market positioning.

---

### TC-04 — VPN user evaluates plans

- **Persona:** A consumer who wants a cheap/free VPN.
- **Goal:** *"As a VPN user, I want to see the plans and prices so I can pick one."*
- **Surface:** Responsive web. **Preconditions:** none.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/vpn](https://iogrid.org/vpn) | Open the **VPN** page | Plans: **Free** (2 GB/mo), **Plus** ($2.99), **Pro** ($4.99) with **Get** buttons | ✅ | [📷 vpn](evidence/tc01-2-vpn-pricing.png) |

- **Journey verdict:** ☑ **PASS** — VPN plans render with the free tier + two paid tiers and clear CTAs.

---

### TC-05 — Sign in with a magic link

- **Persona:** A returning user (provider or customer) — iogrid uses passwordless email sign-in.
- **Goal:** *"As a user, I want to enter my email and get a sign-in link, so that I reach my account without a password."*
- **Surface:** Responsive web. **Preconditions:** an email address. *(This walk used a non-monitored address — final inbox step blocked.)*

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/account](https://iogrid.org/account) | Open the account page signed out | A **"Sign in to iogrid"** card — **Email** field + **Send magic link**. No Google button (correctly hidden until a real OAuth client lands, [#646](https://github.com/iogrid/iogrid/issues/646)/[#653](https://github.com/iogrid/iogrid/issues/653)) | ✅ | [📷 signin](evidence/tc02-1-signin.png) |
| 2 | Sign in to iogrid | Type an email, tap **Send magic link** | Advances to **"Verify Request — check your email"** (no error) | ✅ | [📷 check-email](evidence/tc02-2-check-email.png) |
| 3 | Email inbox | Open the magic-link email, click **Sign in** | Lands authenticated on the account dashboard | ⛔ | — |

- **Journey verdict:** ☒ **BLOCKED** (not a fail) — The on-screen half works: email → **Send magic link** → "check your email" verify-request screen. Step 3 is blocked (non-monitored test inbox, no link access). Re-walk with a monitored inbox to confirm the post-login dashboard + the authenticated provider/customer surfaces (TC-06 below covers the gate they sit behind).

---

### TC-06 — Protected dashboards require sign-in (auth gate)

- **Persona:** A signed-out visitor deep-linking into a dashboard.
- **Goal:** *"As a signed-out user hitting a protected page, I should be sent to sign in, not shown someone's data."*
- **Surface:** Responsive web. **Preconditions:** signed out.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/provider](https://iogrid.org/provider) | Navigate to the provider dashboard signed out | Redirect to **[/account?callbackUrl=/provider](https://iogrid.org/account?callbackUrl=%2Fprovider)** (sign-in, destination preserved) | ✅ | [📷 provider-gate](evidence/tc03-1-provider-authgate.png) |
| 2 | [iogrid.org/customer](https://iogrid.org/customer) | Navigate to the customer dashboard signed out | Redirect to **[/account?callbackUrl=/customer](https://iogrid.org/account?callbackUrl=%2Fcustomer)** (sign-in, destination preserved) | ✅ | [📷 customer-gate](evidence/tc05-2-customer-authgate.png) |

- **Journey verdict:** ☑ **PASS** — Both protected dashboards correctly bounce a signed-out user to sign-in with `callbackUrl` preserved. No provider/customer data leaked to an unauthenticated visitor.

---

### TC-07 — Trust & transparency surfaces (the differentiator)

- **Persona:** A cautious buyer/provider checking iogrid's transparency claims before committing.
- **Goal:** *"As a careful user, I want to see iogrid's transparency, live status, and token economics so I can trust the platform."*
- **Surface:** Responsive web. **Preconditions:** none.

| # | Screen you're on | What you do | What you must see | Result | Evidence |
|---|---|---|---|---|---|
| 1 | [iogrid.org/transparency](https://iogrid.org/transparency) | Open the **Transparency** page | A transparency page (per-byte accounting story) renders | ✅ | [📷 transparency](evidence/tc04-1-transparency.png) |
| 2 | [iogrid.org/status](https://iogrid.org/status) | Open the **Status** page | "System status." page advertising live SLO/uptime "updated every 60s from telemetry-svc", with a **Go to status dashboard** button | ✅ | [📷 status](evidence/tc04-2-status.png) |
| 3 | Status page | Tap **Go to status dashboard** | The live status dashboard at `status.iogrid.org` loads | ❌ | [📷 status-404](evidence/tc04-3-status-dashboard-404.png) |
| 4 | [iogrid.org/token](https://iogrid.org/token) | Open the **$GRID** token page | "$GRID — deflationary work-token on Solana" economics page renders | ✅ | [📷 token](evidence/tc04-4-token.png) |

- **Journey verdict:** ☒ **FAIL** — Steps 1, 2, 4 pass (transparency, status marketing, token economics all render). **Step 3 fails:** the advertised **status dashboard** link dead-ends at a plain **`404 page not found`** (`status.iogrid.org` has no live backend — the iogrid cluster serves via Traefik, not the Gateway-API HTTPRoute declared for it). Filed **[#668](https://github.com/iogrid/iogrid/issues/668) (P2)**. Visibly broken promise on a trust page; not a core-flow blocker.

---

### TC-10 / TC-11 / TC-12 — iOS app (onboarding, VPN connect, top-up) — *NOT WALKED*

- **Surface:** Native iOS app `io.iogrid.app`.
- **Status:** ☐ **NOT WALKED** — there is **no live external TestFlight build** to install on a device yet ([#574](https://github.com/iogrid/iogrid/issues/574)). The journeys are authored in the companion demonstration [`UAT-SAMPLE-mobile-vpn.md`](UAT-SAMPLE-mobile-vpn.md) (Sign in with Apple → connect VPN → Ping top-up), but cannot be given a real verdict without a device + build. Re-walk and capture real device screenshots once the external build is live.

---

## Roll-up

| TC | Surface | Journey | Steps | Walked | ✅ | ❌ | ⛔ | Verdict |
|---|---|---|---|---|---|---|---|---|
| TC-01 | web | Provider eval + download daemon | 2 | 2 | 2 | 0 | 0 | 🟢 PASS |
| TC-02 | web | Customer evaluates 4 workloads | 4 | 4 | 4 | 0 | 0 | 🟢 PASS |
| TC-03 | web | Consolidated pricing | 1 | 1 | 1 | 0 | 0 | 🟢 PASS |
| TC-04 | web | VPN plans | 1 | 1 | 1 | 0 | 0 | 🟢 PASS |
| TC-05 | web | Magic-link sign-in | 3 | 3 | 2 | 0 | 1 | ⛔ BLOCKED (inbox) |
| TC-06 | web | Auth gate (provider+customer) | 2 | 2 | 2 | 0 | 0 | 🟢 PASS |
| TC-07 | web | Trust / transparency / status / token | 4 | 4 | 3 | 1 | 0 | 🔴 FAIL ([#668](https://github.com/iogrid/iogrid/issues/668)) |
| TC-10–12 | iOS app | Onboarding / VPN / top-up | — | 0 | 0 | 0 | 0 | ☐ NOT WALKED ([#574](https://github.com/iogrid/iogrid/issues/574)) |
| | | **Total (web)** | **17** | **17** | **15** | **1** | **1** | |

**Overall verdict:** ⚠️ **CONDITIONAL** — iogrid's core web surfaces are solid and go-live-ready: provider download, all four customer workloads with real pricing, VPN plans, the magic-link sign-in *request*, and auth-gating all PASS. One **P2 defect** ([#668](https://github.com/iogrid/iogrid/issues/668)): the `/status` page's "Go to status dashboard" link 404s. One **inbox-gated ⛔** (TC-05 completion — a walk-setup limit, not a defect). The **iOS app is NOT WALKED** pending an external TestFlight build ([#574](https://github.com/iogrid/iogrid/issues/574)). No P0/P1 found on web.

---

## Defects found during this walk

| Defect | Step | What the user saw | Severity | Ticket |
|---|---|---|---|---|
| `/status` "Go to status dashboard" → `status.iogrid.org` 404s | TC-07.3 | Plain `404 page not found` instead of the live status dashboard | P2 | [#668](https://github.com/iogrid/iogrid/issues/668) |

> The home page also logs a cosmetic `favicon.ico 404` in the browser console — no on-screen surface, so per the golden rule it is **not** a UAT row (noted for the dev team only).

---

## Out of scope (handled by the dev team, NOT walked here)

Unit/integration/contract tests, CI pipelines, API/`kubectl`/SQL/log verification, source reading. Capabilities with no on-screen surface have no UAT row.

---

_Filled from [`UAT-TEMPLATE.md`](UAT-TEMPLATE.md) v1. Web journeys walked live 2026-06-03; iOS pending a device build ([#574](https://github.com/iogrid/iogrid/issues/574)). Every walked row is one real action with a committed screenshot._
