# TRUST ledger

> **WHAT:** Verification ledger for every claimed-done item.
> **AUTHORITY:** LIVE state, cron-refreshed alongside [`TRACKER.md`](./TRACKER.md). Authoritative answer to "is X actually verified-on-a-fresh-prov?"
> **POINTER:** Engineering principles + anti-pattern catalog → [`../PRINCIPLES.md`](../PRINCIPLES.md) (when written) or repo-level [`../../CLAUDE.md`](../../CLAUDE.md) §3.

Every claimed-done item is in one of four states:

| State | Meaning |
|---|---|
| 🔴 **UNVERIFIED** (default) | Code shipped + CI green. No walk yet. |
| 🟢 **VERIFIED-PASS** | Operator walked the surface on a fresh prov; screenshot attached to the issue. |
| ⛔ **VERIFIED-FAIL** | Walk failed; the surface is broken even though CI was green. |
| 🟡 **VERIFIED-PARTIAL** | Some sub-surfaces pass, others fail. |

A new PR against a claimed-done surface flips it back to 🔴 UNVERIFIED.

## Current entries

(Populate as walks happen. Suggested format below.)

| Surface | State | Last walk | Evidence | Notes |
|---------|-------|-----------|----------|-------|
| `/account/identifiers` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#371 comment](https://github.com/iogrid/iogrid/issues/371#issuecomment-4503922302) | PR #372 |
| `/provide/earnings` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#324 comment](https://github.com/iogrid/iogrid/issues/324#issuecomment-4504153780) | PR #330 + #379 |
| `/account/wallets` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#326 comment](https://github.com/iogrid/iogrid/issues/326#issuecomment-4504154098) | PR #341 |
| `/account/sessions` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#322 comment](https://github.com/iogrid/iogrid/issues/322#issuecomment-4504566224) | PR #336 |
| `/provide/audit` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#323 comment](https://github.com/iogrid/iogrid/issues/323#issuecomment-4504566384) | PR #366 |
| GeoIP populates country/region | 🟢 VERIFIED-PASS | 2026-05-21 | `provider-a7a93576` reconnect post-rollout: `public_ip=188.66.253.46 country_code=OM region_name=Muscat` ([providers-svc log](https://github.com/iogrid/iogrid/issues/381)) | Refs #381 — Traefik chart revision 8 = ETP=Local + replicas=2 + forwardedHeaders.insecure=true |
| EPIC #309 end-to-end hatice walk | 🟢 VERIFIED-PASS | 2026-05-21 | [#309 comment](https://github.com/iogrid/iogrid/issues/309#issuecomment-4504157052) | 7 surfaces green |
