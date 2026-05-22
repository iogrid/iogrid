# Walk evidence — 2026-05-23 21:05Z

Live operator walk of `iogrid.org` + `admin.iogrid.org` AFTER the 7-PR
merge train (#445–#451) but BEFORE the new images can deploy — the
cluster's `ghcr-pull` token is revoked (#454), so every coordinator
service is still running its pre-merge image. The walk therefore
captures the PRE-MERGE-EFFECTIVE state, which is what end-users see
right now.

| # | Screenshot | URL | Observed state | Post-PR-#445 + #454-rotate target |
|---|---|---|---|---|
| 01 | [01-iogrid-landing.png](01-iogrid-landing.png) | `https://iogrid.org/` | h1: *"Rent your idle machine."* (PR #423 thin Linear) | h1: *"The mesh that shows you every byte."* + restored Hero + Stats + 4-card FeatureGrid + anti-Hola moat + ComparisonTable + vCard case study |
| 02 | [02-iogrid-install.png](02-iogrid-install.png) | `https://iogrid.org/install` | HTTP 200, PortalShell-wrapped (no MarketingShell Nav + Footer) | HTTP 200, MarketingShell-wrapped + InstallButton OS-detect island |
| 03 | [03-welcome-404.png](03-welcome-404.png) | `https://iogrid.org/welcome` | **HTTP 404** | HTTP 200, three-card PersonaPickerCard picker |
| 04 | [04-admin-503.png](04-admin-503.png) | `https://admin.iogrid.org/` | **HTTP 503** (admin pods in `ImagePullBackOff` — #426) | HTTP 200, brand-palette + left-rail |

Additional checks (HTTP-only, no screenshot):
- `https://iogrid.org/providers` — HTTP 404 today; post-merge will be 200 (marketing page)
- `https://iogrid.org/vpn` — HTTP 307 → /install today; post-merge will be 200 (vpn marketing)
- `https://iogrid.org/provider` — HTTP 404 today; post-merge will be 200 (AppShell)
- `https://iogrid.org/provide` — HTTP 200 today; post-merge will be 301 → /provider

## What unblocks the post-merge walk

1. **#454** — founder mints fresh PAT + `kubectl create secret docker-registry ghcr-pull` rotate. Until then `ErrImagePull` on every coordinator service that tries to roll. Post-rotation: `kubectl rollout restart` on identity-svc / billing-svc / web / admin and the new images pull cleanly.
2. **#426** — founder flips `ghcr.io/iogrid/admin` package visibility to public via github.com Org → Packages settings. Until then admin pods stay `ImagePullBackOff` (separate symptom from #454 — admin package is private without an explicit pull access grant).

Sequence: #454 → #426 → 30s `rollout restart` → walk this same set of URLs again, screenshots in `2026-05-24-walk-evidence/` (or whenever the rotation lands).

## Console errors

iogrid.org/ has 7 console errors visible — likely from the post-#445 image NOT being deployed (the live web pod is running an older bundle that references assets that have since changed). They should clear once the new web image rolls.
