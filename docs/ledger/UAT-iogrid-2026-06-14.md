# UAT — iogrid (authenticated web) — 2026-06-14

> **Fresh, deep, authenticated User-Acceptance-Test walk** of live `iogrid.org` + `admin.iogrid.org`, run with **Playwright (Chromium)**. Supersedes [`UAT-iogrid-2026-06-03.md`](UAT-iogrid-2026-06-03.md) (11 days stale; it predated every G1/G2/G3 fix and the deploy reconcile that brought 7 coordinator services current).
>
> **Golden rule — 100% the real end-user experience.** Every row below is a real navigate + screenshot + console/network capture against prod, signed in as a real user. No fabricated passes. Every ⚠️/❌ has a filed issue.

---

## Metadata

| Field | Value |
|---|---|
| **Product / release** | iogrid — web (provider + customer + VPN + account) + admin staff console |
| **Build under test** | live `iogrid.org` / `admin.iogrid.org`; web image `harbor.openova.io/iogrid/web@sha256:fb81e56c…1871e0c`; repo @ `5244365e` |
| **Environment** | [https://iogrid.org](https://iogrid.org) + [https://admin.iogrid.org](https://admin.iogrid.org), signed in as `emrah.baysal@openova.io` |
| **Surface(s)** | Authenticated responsive web (Chromium / Playwright) |
| **Tester** | `@p0-474-lonely` (UAT executor) |
| **Walk date** | 2026-06-14 |
| **Auth method** | Live authenticated NextAuth session (`emrah.baysal@openova.io`) — verified live by walking data-backed surfaces with no bounce to the sign-in gate, and by `GET /api/v1/me` → 200 returning the real account. The read-only Stalwart IMAP magic-link path (`mail.openova.io:993`, `SELECT INBOX readonly`) was prepared and verified reachable as a fallback but was not needed. |
| **Overall verdict** | 🟢 **PASS with watch-items — 7/7 surfaces walked, 6 ✅ / 1 ⚠️ (account).** G3 admin earnings view PROVEN LIVE (Hatice's Mac = **12.325 $GRID / 17 builds**, grown past the 11.05/14 in the memory note). Two web defects filed (#801 identifiers render, #802 billing 501) — both degraded-not-broken. |

---

## Result legend

✅ works · ⚠️ degraded (renders, but a real gap) · ❌ broken · ⛔ not walked (auth-gated / tooling)

---

## Per-surface results

| # | Surface | Status | Evidence | Notes |
|---|---|---|---|---|
| 1 | `iogrid.org/` (landing) | ✅ works | [01-landing.png](evidence/uat-2026-06-14/01-landing.png) | Hero, live-stats (honest dashes pre-Phase-1), competitive-matrix table, Dynolabs vCard first-customer case study, pricing CTA, footer. 0 console errors. |
| 1b | `/welcome` (onboarding) | ✅ works | [02-welcome.png](evidence/uat-2026-06-14/02-welcome.png) | 3-role chooser (Provider / Customer / VPN), each card with copy + CTA. 0 console errors. |
| 2 | `/provider` (dashboard, earnings, per-byte transparency) | ✅ works | [04-provider-overview.png](evidence/uat-2026-06-14/04-provider-overview.png) · [05-provider-earnings.png](evidence/uat-2026-06-14/05-provider-earnings.png) · [06-provider-transparency.png](evidence/uat-2026-06-14/06-provider-transparency.png) | **Richer than 2026-06-03** — a real **paired daemon** (`c0138910…e0f0`, Active) now shows, with a live transparency feed of real scheduler state-change events. `dashboard` + `earnings/summary` → 200. Earnings page: lifetime/30d/pending/next-payout cards (7 workloads), Daily/Weekly/Monthly tabs, Withdraw, **multi-currency payout picker** (Hold \$GRID / Cash via Stripe Connect / Donate to charity). Transparency: filter tabs + **Live** badge + Pause/Clear/Export-CSV; `audit/stream` now **200** (was `404 no_provider` pre-pairing). 0 console errors. |
| 3 | `/customer` (proxy/compute/GPU/iOS-build console) | ✅ works | [07-customer-overview.png](evidence/uat-2026-06-14/07-customer-overview.png) · [08-customer-workloads.png](evidence/uat-2026-06-14/08-customer-workloads.png) · [09-customer-usage.png](evidence/uat-2026-06-14/09-customer-usage.png) · [11-customer-apikeys.png](evidence/uat-2026-06-14/11-customer-apikeys.png) | Workspace summary cards. Workloads submit form + filter expose all 4 types: **Bandwidth (residential) / Docker container / GPU compute / iOS build** (the first-class iOS-build differentiator). Usage page: `GET /api/v1/customer/usage` → **200** (was 501 in 2026-06-03; #675 fix holds), Bytes/Cost/Records + type filter + honest empty state. API keys: create form + 2 real keys (masked `iog_…`) + Revoke. 0 console errors. *(Read-only: did not submit a workload or mint/revoke a key — prod-mutation disallowed this walk; those flows were proven 2026-06-03.)* |
| 4 | `/account` (wallet / payouts / multi-currency) | ⚠️ degraded | [03-account-profile-authenticated.png](evidence/uat-2026-06-14/03-account-profile-authenticated.png) · [12-account-wallets.png](evidence/uat-2026-06-14/12-account-wallets.png) · [13-account-identifiers-empty-BUG.png](evidence/uat-2026-06-14/13-account-identifiers-empty-BUG.png) · [14-account-sessions.png](evidence/uat-2026-06-14/14-account-sessions.png) | **Profile ✅** (real name + email, sign-out). **Wallets ✅** (bind UI: "No wallets bound", Connect/Connect-&-bind, balance card, 20%-\$GRID-discount copy — actual Phantom-signed bind needs the extension, a tooling limit not a defect). **Identifiers ⚠️ → [#801](https://github.com/iogrid/iogrid/issues/801)**: the list renders **EMPTY** even though `GET /api/v1/me` (200) returns a verified `emrah.baysal@openova.io` identifier (`kind:2`) — a web render regression of #685, the proto3-enum-as-int masking class. **Sessions ⚠️** (folded into #801): shows "Unknown device · Expired" instead of flagging the current browser as "this device" (regressed #685 watch-item). No crashes / no console errors — degraded, not broken. |
| 5 | `/vpn` (VPN management) | ✅ works | [15-vpn.png](evidence/uat-2026-06-14/15-vpn.png) · [16-customer-vpn.png](evidence/uat-2026-06-14/16-customer-vpn.png) | Marketing `/vpn`: hero, pricing (Free 2 GB / Plus \$2.99 / Pro \$4.99), What-it-is / How-it-works / Pricing / FAQ / Get-started, CTAs (Install / Mint VPN key / Upgrade). Authenticated `/customer/vpn`: Active sessions + CLI connect hint, VPN keys table + Mint button, install one-liner, Windows .msi link. 0 console errors on both. |
| 6 | `admin.iogrid.org/` + `/providers` (**G3 proof**) | ✅ works | [17-admin-overview.png](evidence/uat-2026-06-14/17-admin-overview.png) · [18-admin-providers-G3-grid-earnings.png](evidence/uat-2026-06-14/18-admin-providers-G3-grid-earnings.png) | **G3 LIVE.** Staff console authenticated as `emrah.baysal@openova.io` (gated by `is_admin` + `IOGRID_ADMIN_EMAILS`). Providers table renders **`Hatices-Mac-mini-2` (`808ce330…`) — active — Settled `12.325 $GRID` — `17` Builds** — last seen 6/14 16:53. `/api/v1/providers/list` + per-provider `/earnings` → 200. The on-chain provider-payout pipeline is proven on the operator surface; numbers have **grown past** the 11.05 \$GRID / 14 builds in the project memory (pipeline kept settling). 0 console errors. |

---

## Top-line verdict

🟢 **PASS with watch-items — 7/7 surfaces walked: 6 ✅ works, 1 ⚠️ degraded (`/account`).** 0 ⛔ not-walked.

- The walk ran **fully authenticated** (`emrah.baysal@openova.io`) — no surface was skipped for auth.
- **G3 is proven live on the admin earnings surface** (12.325 \$GRID / 17 builds attributed to Hatice's Mac), the single most important thing this UAT had to confirm.
- The provider dashboard is materially **better than 2026-06-03**: a real paired daemon + a live transparency feed now render (the EventSource `audit/stream` is 200, not the old `404 no_provider`).
- The 2026-06-03 prod fixes **held**: customer usage is 200 (not 501), the customer console is clean.
- Two genuine web defects found, both **degraded-not-broken** (pages render, no crashes): #801 (account identifiers list empty despite valid backend data — a #685 regression in the proto-enum masking class) and #802 (billing page fires a 501 to an Unimplemented `SubscriptionService.GetSubscription` stub).

## Issues filed

| Issue | Surface | Severity | Summary |
|---|---|---|---|
| [#801](https://github.com/iogrid/iogrid/issues/801) | `/account/identifiers` (+ `/account/sessions`) | P2 | Identifiers list renders **empty** despite `GET /api/v1/me` (200) returning a verified `kind:2` email identifier — web render regression of #685, proto3-enum-as-int masking class. Sessions "this device" row also regressed (folded in). |
| [#802](https://github.com/iogrid/iogrid/issues/802) | `/customer/billing` | P3 | Billing page fires `GET /api/v1/vpn/account` → **501** `unimplemented: SubscriptionService.GetSubscription is not implemented` (Unimplemented Connect stub, #686 class). Degraded — page renders the correct prepaid-\$GRID empty state. (The `billing/balance` 409 `no_wallet_bound` is by-design, not a defect.) |

---

## Method notes (for the next walk)

- **Auth.** A live NextAuth session for `emrah.baysal@openova.io` was already established in the Playwright profile; liveness was confirmed by walking data-backed surfaces with no sign-in bounce and by `GET /api/v1/me` → 200 (real account). The canonical magic-link-via-read-only-IMAP fallback (`mail.openova.io:993`, secret `stalwart-emrah-real` key `emrah`, `SELECT INBOX readonly`) was prepared + verified reachable but not needed. **No mail was read, modified, sent, or deleted.**
- **Read-only on prod data.** No workload submitted, no API key minted/revoked, no wallet bound, no VPN daemon touched, nothing deployed — per the walk constraints. Mutating flows (key create/revoke, workload submit, money path) were already proven in the 2026-06-03 UAT.
- **By-design "errors" that are NOT defects.** `billing/balance` → 409 `no_wallet_bound` (no wallet bound → correct empty state); wallet bind needs the Phantom extension (headless tooling limit). Both render correct UX.

---

_Walked live authenticated 2026-06-14 with Playwright/Chromium. Evidence under [`evidence/uat-2026-06-14/`](evidence/uat-2026-06-14/). Supersedes [`UAT-iogrid-2026-06-03.md`](UAT-iogrid-2026-06-03.md)._
