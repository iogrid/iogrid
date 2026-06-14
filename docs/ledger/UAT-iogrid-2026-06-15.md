# UAT — iogrid (authenticated web) — 2026-06-15

> **Evidence-based re-walk** of live `iogrid.org` + `admin.iogrid.org`, run with **Playwright (Chromium)**, signed in as `emrah.baysal@openova.io`. This is the verification pass that closes out the three web defects flagged degraded in [`UAT-iogrid-2026-06-14.md`](UAT-iogrid-2026-06-14.md) — all three merged + auto-deployed via the image-reroll cron and are now **proven live**.
>
> **Golden rule — 100% the real end-user experience.** Every row is a real navigate + screenshot + network/console capture against prod, signed in as a real user. No fabricated passes. Every ⚠️/❌ keeps/gets a filed issue.

---

## Metadata

| Field | Value |
|---|---|
| **Product / release** | iogrid — web (provider + customer + VPN + account) + admin staff console |
| **Build under test** | live `iogrid.org` / `admin.iogrid.org`. Running images: `web@sha256:61edc9e7…` (CI `3399abc6`), `gateway-bff@sha256:e8be5aa1…` (CI `5c8f04f3`), `billing-svc@sha256:c60767f0…` (CI `5c8f04f3`) — all confirmed to contain the three fix commits below (see §Deploy verification). |
| **Environment** | [https://iogrid.org](https://iogrid.org) + [https://admin.iogrid.org](https://admin.iogrid.org), signed in as `emrah.baysal@openova.io` |
| **Surface(s)** | Authenticated responsive web (Chromium / Playwright) |
| **Tester** | `@p0-474-lonely` (UAT executor) |
| **Walk date** | 2026-06-15 |
| **Auth method** | Live authenticated NextAuth session for `emrah.baysal@openova.io` (carried in the Playwright profile from the prior walk). Liveness confirmed by `GET /api/v1/me` → 200 returning the real account + a verified `IDENTIFIER_KIND_MAGIC_LINK` identifier, and by walking every data-backed surface with no bounce to the sign-in gate. The read-only Stalwart IMAP magic-link fallback (`mail.openova.io:993`, `SELECT INBOX readonly`) was available but **not needed; no mail was read, modified, sent, or deleted**. |
| **Overall verdict** | 🟢 **PASS — 7/7 surfaces ✅, 0 ⚠️ / 0 ❌.** All three 2026-06-14 web defects (#801 identifiers, #808 sessions, #802 billing 501) verified **FIXED live**. G3 admin earnings still proven (**12.325 $GRID / 17 builds** on Hatice's Mac). #801 / #802 / #808 are now evidence-closeable. |

---

## Deploy verification (STEP 1)

The three fixes merged 2026-06-14 ~19:28–20:09 (+0400); the restored image-reroll cron (`5,20,35,50`) deploys every ~15 min. Running images were confirmed on the bastion (`kubectl -n iogrid get deploy …`) and correlated to their CI commits via the in-repo deploy markers, then each fix commit's ancestry was checked against the CI commit that built the running image:

| Deploy | Running image | Built after CI | Contains fix? |
|---|---|---|---|
| `web` | `sha256:61edc9e7…` | `3399abc6` | **#808** (`3399abc6`, the commit itself) ✅ · **#801 web-side** (`63bafd84`, ancestor of `3399abc6`) ✅ |
| `gateway-bff` | `sha256:e8be5aa1…` | `5c8f04f3` | **#801 bff-side** (`63bafd84`, ancestor of `5c8f04f3`) ✅ |
| `billing-svc` | `sha256:c60767f0…` | `5c8f04f3` | **#802** (`e69d3ce0`, ancestor of `5c8f04f3`) ✅ |

Fix commits: `e69d3ce0` (#802 GetSubscription), `63bafd84` (#801 protojson `/api/v1/me`), `3399abc6` (#808 sessions "this device"). All three pods `1/1 Running`, post-fix ages 21–37 min at walk time. **No stale-pod risk — the walk ran against post-fix images.**

---

## Result legend

✅ works · ⚠️ degraded (renders, but a real gap) · ❌ broken · ⛔ not walked (auth-gated / tooling)

---

## Per-surface results

| # | Surface | 2026-06-14 | 2026-06-15 | Evidence | Notes |
|---|---|---|---|---|---|
| 1 | `/account/identifiers` (**#801**) | ⚠️ empty | ✅ **FIXED** | [01-account-identifiers-FIXED.png](evidence/uat-2026-06-15/01-account-identifiers-FIXED.png) | The verified-email row now **renders**: `emrah.baysal@openova.io` · `Magic-link email` · `Verified` · Remove. Backend proof: `GET /api/v1/me` → 200 now returns `"kind":"IDENTIFIER_KIND_MAGIC_LINK"` + `"verifiedEmail":"emrah.baysal@openova.io"` (the protojson enum-as-string the #801 fix added — previously masked as `kind:2`, which is why the list rendered empty). 0 console errors. |
| 2 | `/account/sessions` (**#808**) | ⚠️ "Unknown device · Expired" | ✅ **FIXED** | [02-account-sessions-FIXED.png](evidence/uat-2026-06-15/02-account-sessions-FIXED.png) | The current browser now renders as **`Chrome on Linux` · `Current session` · `never used · expires in 30d`** — exactly the #808 fix. (A separate older `Unknown device · Expired` row with a Revoke button is a genuine stale/expired session, correctly **not** flagged as current.) 0 console errors. |
| 3 | `/customer/billing` (**#802**) | ⚠️ 501 | ✅ **FIXED** | [03-customer-billing-FIXED.png](evidence/uat-2026-06-15/03-customer-billing-FIXED.png) | On clean page load `GET /api/v1/vpn/account?workspace_id=… → 200` (was **501 `unimplemented: SubscriptionService.GetSubscription`**). Body: `{"tier":"FREE","status":"active","bandwidth_used_bytes":0,"bandwidth_quota_bytes":2147483648,"upgrade_available":true}` — **FREE tier, 2 GB quota**. Page renders the prepaid-$GRID empty state. The only remaining call is `billing/balance → 409 no_wallet_bound`, the documented **by-design** empty state (no wallet bound), not a defect. |
| 4 | `/provider` (dashboard, earnings, transparency) | ✅ | ✅ works | [04-provider-overview.png](evidence/uat-2026-06-15/04-provider-overview.png) | Paired daemon `c0138910…e0f0` (Active), Scheduler Active / Accepting workloads, Earnings + Bandwidth + CPU/Mem cards, live Recent-activity transparency feed with real scheduler state-change events. 0 console errors. |
| 5 | `/customer` (proxy/compute/GPU/iOS-build console) | ✅ | ✅ works | [05-customer-overview.png](evidence/uat-2026-06-15/05-customer-overview.png) | Workspace summary (Spend / Bytes / Workload-types), Usage-by-type panel + honest empty state, API-keys / Workloads / Billing nav. 0 console errors. *(Read-only: no workload submitted, no key minted — proven in earlier UATs.)* |
| 6 | `/vpn` (`/customer/vpn`) | ✅ | ✅ works | [06-customer-vpn.png](evidence/uat-2026-06-15/06-customer-vpn.png) | Active-sessions panel + CLI connect hint, VPN-keys table (real key `cli-unblock`) + Mint, install one-liner, Windows `.msi` link. 0 console errors. |
| 7 | `admin.iogrid.org/providers` (**G3 proof**) | ✅ (12.325/17) | ✅ works | [07-admin-providers-G3-grid.png](evidence/uat-2026-06-15/07-admin-providers-G3-grid.png) | **G3 STILL LIVE.** Provider pool renders `Hatices-Mac-mini-2` (`808ce330…`, owner `a7a93576…`) — **active — Settled `12.325 $GRID` — `17` Builds** — last seen 6/15 01:00. (A second daemon row `c0138910…` / owner `18c9fd5d…` shows `0 $GRID / 0 builds` — the operator's own un-monetized pairing.) Only console noise = `404 favicon.ico` (cosmetic). $GRID figure **unchanged** from 2026-06-14 (no new builds settled since; pipeline stable). |

---

## Top-line verdict

🟢 **PASS — 7/7 surfaces ✅ works. 0 ⚠️ / 0 ❌ / 0 ⛔.**

- The walk ran **fully authenticated** (`emrah.baysal@openova.io`); no surface skipped for auth.
- **All three 2026-06-14 web defects are verified FIXED live**, each with a backend receipt (not just a screenshot):
  - **#801** — `/api/v1/me` → 200 now emits `kind:"IDENTIFIER_KIND_MAGIC_LINK"` (protojson enum-as-string), and `/account/identifiers` lists the verified email row.
  - **#808** — `/account/sessions` flags the current browser as `Chrome on Linux · Current session`.
  - **#802** — the GetSubscription-backed `vpn/account` → **200 `tier:FREE`** (was 501); `/customer/billing` renders the FREE-tier prepaid empty state.
- **G3 remains proven on the admin earnings surface** — **12.325 $GRID / 17 builds** attributed to Hatice's Mac (`808ce330…`), the most important number this UAT had to re-confirm.
- The four spot-confirm surfaces (`/provider`, `/customer`, `/vpn`, admin `/providers`) all still render clean.

## Issues — status after this walk

| Issue | Surface | Prior | Now | Evidence |
|---|---|---|---|---|
| [#801](https://github.com/iogrid/iogrid/issues/801) | `/account/identifiers` | ⚠️ open (P2) | ✅ **fixed-live** (closeable) | `GET /api/v1/me` enum-as-string + identifiers row renders |
| [#808](https://github.com/iogrid/iogrid/issues/808) | `/account/sessions` | ⚠️ open | ✅ **fixed-live** (closeable) | "Chrome on Linux · Current session" renders |
| [#802](https://github.com/iogrid/iogrid/issues/802) | `/customer/billing` | ⚠️ open (P3) | ✅ **fixed-live** (closeable) | `vpn/account → 200 tier:FREE` (was 501) |

No new defects found. No surface regressed.

---

## Method notes

- **Auth.** Live NextAuth session for `emrah.baysal@openova.io`, liveness confirmed via `GET /api/v1/me` → 200 (real account + verified identifier). Read-only IMAP magic-link fallback prepared but **not used; no mail touched**.
- **Read-only on prod data.** No workload submitted, no API key minted/revoked, no wallet bound, **VPN daemon not touched**, nothing deployed — per walk constraints. Backend statuses (200/409) were read by re-issuing **GET-only** `fetch()` from the already-authenticated page; no mutation.
- **By-design "errors" that are NOT defects.** `billing/balance → 409 no_wallet_bound` (no wallet bound → correct empty state); admin `404 favicon.ico` (cosmetic). Both render correct UX.

---

_Walked live authenticated 2026-06-15 with Playwright/Chromium. Evidence under [`evidence/uat-2026-06-15/`](evidence/uat-2026-06-15/). Verification pass for the three defects flagged in [`UAT-iogrid-2026-06-14.md`](UAT-iogrid-2026-06-14.md)._
