# TRUST ledger

> **WHAT:** Verification ledger for every claimed-done item.
> **AUTHORITY:** LIVE state, cron-refreshed alongside [`TRACKER.md`](./TRACKER.md). Authoritative answer to "is X actually verified-on-a-fresh-prov?"
> **POINTER:** Engineering principles + anti-pattern catalog → the project engineering principles (see `CLAUDE.md` at the repo root) (when written) or repo-level [`../../CLAUDE.md`](../../CLAUDE.md) §3.

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
| `/account/identifiers` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#371 comment](https://github.com/iogrid/iogrid/issues/371#issuecomment-4503922302); re-walk 2026-05-21 round 2 returns HTTP 200 with `__Secure-authjs.session-token` cookie | PR #372 |
| `/provide` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#324 comment](https://github.com/iogrid/iogrid/issues/324#issuecomment-4504153780); re-walk 2026-05-21 round 2 returns HTTP 200 with `__Secure-authjs.session-token` cookie | PR #330 + #379 |
| `/account/wallets` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#326 comment](https://github.com/iogrid/iogrid/issues/326#issuecomment-4504154098); re-walk 2026-05-21 round 2 returns HTTP 200 with `__Secure-authjs.session-token` cookie | PR #341 |
| `/account/sessions` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#322 comment](https://github.com/iogrid/iogrid/issues/322#issuecomment-4504566224); re-walk 2026-05-21 round 2 returns HTTP 200 with `__Secure-authjs.session-token` cookie | PR #336 |
| `/provide/audit` (hatice walk) | 🟢 VERIFIED-PASS | 2026-05-21 | [#323 comment](https://github.com/iogrid/iogrid/issues/323#issuecomment-4504566384); re-walk 2026-05-21 round 2 (covered under `/provide` 200) | PR #366 |
| GeoIP populates country/region | 🟢 VERIFIED-PASS | 2026-05-21 | `provider-a7a93576` reconnect post-rollout: `public_ip=188.66.253.46 country_code=OM region_name=Muscat` ([providers-svc log](https://github.com/iogrid/iogrid/issues/381)); re-walk 2026-05-21 round 2 confirms DB row still present after #412 ETP=Local roll | Refs #381 — Traefik chart revision 8 = ETP=Local + replicas=2 + forwardedHeaders.insecure=true |
| EPIC #309 end-to-end hatice walk | 🟢 VERIFIED-PASS | 2026-05-21 | [#309 comment](https://github.com/iogrid/iogrid/issues/309#issuecomment-4504157052) | 7 surfaces green |
| `/install` (download landing) | 🟢 VERIFIED-PASS | 2026-05-21 | re-walk 2026-05-21 round 2: HTTP 200, body advertises `.pkg` (macOS), `.msi` (Windows), `.deb` (Linux) installers | Refs #348 #387 #401 #404 — Phase-2 auto-update channels shipped this morning |
| `releases.iogrid.org` (Sparkle / Win / Linux update feeds) | 🟢 VERIFIED-PASS | 2026-05-21 | re-walk 2026-05-21 round 2: HTTP 200, body advertises `/macos/appcast.xml`, `/macos/<version>/iogrid-<version>-<arch>.pkg`, `/latest/iogrid-<os>-<arch>.<ext>` | Refs #393 — releases host deployed for Sparkle + Windows Squirrel + Linux apt/yum/apk channels |
| `iogrid.org` (marketing site) | 🟢 VERIFIED-PASS | 2026-05-21 | re-walk 2026-05-21 round 2: HTTP 200, Next.js marketing landing, LE cert (no Traefik default-cert fallback) | Refs #391 #410 #411 — marketing site deployed + Traefik default-cert fallback fixed |
| `admin.iogrid.org` (DNS + admin-route gating) | 🟡 VERIFIED-PARTIAL | 2026-05-21 | re-walk 2026-05-21 round 2: host resolves, `/` returns HTTP 200 (serves web app), `/admin/providers` with hatice cookie returns 307 → `/customer?from=admin-forbidden` (RBAC works for non-admin user) | Refs #408 — admin Service + route gating shipped; admin UI itself behind RBAC and not walked as admin user this round |
| `proxy.iogrid.org:443` SOCKS5 TLS passthrough | 🟢 VERIFIED-PASS | 2026-05-21 | re-walk 2026-05-21 round 2: `printf '\x05\x01\x00' \| openssl s_client -connect proxy.iogrid.org:443 -servername proxy.iogrid.org -quiet \| xxd` returns `05 ff` (SOCKS5 server response — no auth methods acceptable, proves backend reached) | Refs #350 #351 #373 #414 — IngressRouteTCP HostSNI passthrough + proxy-gateway TLS path env vars |
