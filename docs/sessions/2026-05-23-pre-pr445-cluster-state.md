# Pre-PR-#445 Live Cluster State (2026-05-23 06:20Z)

> 🗄️ Historical — captured 2026-05-23; transient pre-merge baseline snapshot,
> does NOT reflect current state (the #445–#451 merge train + many later
> deploys have since landed). For current state see `docs/ledger/TRACKER.md`.

> Captured before the 7-PR merge train (#445–#451) lands so post-merge
> walks can compare against this baseline. **What** to expect when the
> founder runs the operator walk after merge. Date-stamped per §11.

## URL state snapshot

| URL | Pre-merge | Expected post-merge |
|---|---|---|
| `https://iogrid.org/` | HTTP 200, h1 = *"Rent your idle machine."* (PR #423's thin Linear) | h1 = *"The mesh that shows you every byte."* (PR #445 restored Hero) |
| `https://iogrid.org/install` | HTTP 200, h1 = *"Install iogrid"* via PortalShell | HTTP 200, same h1 wrapped in MarketingShell (Nav + Footer) |
| `https://iogrid.org/providers` | **HTTP 404** | HTTP 200, /providers marketing page restored by PR #445 |
| `https://iogrid.org/vpn` | HTTP 307 → /install (legacy redirect) | HTTP 200, /vpn marketing page (Free 2GB / Plus / Pro) |
| `https://iogrid.org/welcome` | HTTP 404 | HTTP 200, three-card persona picker |
| `https://iogrid.org/provide` | HTTP 200 | HTTP 301 → /provider |
| `https://iogrid.org/provider` | HTTP 404 | HTTP 200, AppShell-wrapped overview |
| `https://admin.iogrid.org/` | **HTTP 503** (#426 ghcr block) | HTTP 200 once #426 founder-flip lands |
| `proxy.iogrid.org:443` (TLS SNI) | Serves `app.iogrid.org` cert (#414 entrypoint config) | Same — #414 needs platform-repo Traefik fix |

## Cluster pod state

```
$ kubectl get pods -n iogrid 2>&1 | head
NAME                             READY   STATUS             RESTARTS       AGE
admin-5b8696dbf4-p4p8l           0/1     ImagePullBackOff   0              30h
admin-6d794c9dc-qkhrd            0/1     ImagePullBackOff   0              30h
admin-b8fc5f758-g9jfm            0/1     ImagePullBackOff   0              30h
antiabuse-svc-568d777666-bjqvb   1/1     Running            0              37h
billing-svc-fccdc9bc-v2dvm       1/1     Running            0              37h
gateway-bff-59dddb9645-f2j6b     1/1     Running            0              37h
identity-svc-7f95d6d85-5fbhf     1/1     Running            0              37h
iogrid-pg-1                      1/1     Running            7 (2d2h ago)   3d6h
providers-svc-6f68d554c9-krwrq   1/1     Running            0              33h
proxy-gateway-884cdb975-9t5x2    1/1     Running            0              30h
releases-6f7fd8d64d-5czvk        1/1     Running            0              36h
releases-6f7fd8d64d-n9m9p        1/1     Running            0              36h
web-8556899cc7-h79vf             1/1     Running            0              23h
workloads-svc-f95c6f68b-ghvmt    1/1     Running            0              37h
```

## DB state (providers)

```
$ kubectl -n iogrid exec iogrid-pg-1 -c postgres -- psql -U postgres -d providers \
    -c "SELECT id, display_name, public_ip, country_code, region_name, last_seen_at FROM providers LIMIT 5;"

                  id                  |   display_name    |   public_ip   | country_code | region_name |         last_seen_at
--------------------------------------+-------------------+---------------+--------------+-------------+-------------------------------
 808ce330-79c1-4390-8cc6-87c5ce5a94d8 | Hatice's Mac      |               |              |             | 2026-05-19 20:29:14.17037+00
 cac83611-4a6f-4937-95b4-8f4fb2538808 | provider-a7a93576 | 188.66.253.46 | OM           | Muscat      | 2026-05-22 20:12:32.524567+00
```

`provider-a7a93576` (the bastion's heartbeating stub) shows live GeoIP
+ last_seen_at advancement (#311 #381 RESOLVED-LIVE).
`Hatice's Mac` (808ce330-…) hasn't re-paired since 2026-05-19; will
backfill on the next pair.

## Boot warnings to expect to clear

| Warning | Cleared by |
|---|---|
| `solana: stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset)` | #274 + #345 (founder-physical Solana faucet) |
| `NATS_URL not set — metering consumer disabled` | NATS StatefulSet apply (IaC ready at `infra/k8s/base/data/nats.yaml`); env wired this session |
| `JWT_KEYPAIR_AUTOGEN=1 — generated EPHEMERAL keypair` | #452 (founder runs `scripts/identity-svc-jwt-keypair-gen.sh` + kubeseal) |
| `siws: redis unavailable; using in-memory challenge store` | #453 (Redis StatefulSet apply); env wired this session |
| `GSB_API_KEY unset` / `PHOTODNA_API_KEY unset` / `PHISHTANK_API_KEY unset` | Founder-physical: external service partnerships + API key purchase |

## What the operator walk should produce post-merge

For EPIC #422 closure (the canonical 5-pillar walk):

1. **Marketing landing** — `iogrid.org/` shows the rich Hero + Stats + 4-card FeatureGrid + anti-Hola moat band + ComparisonTable + vCard case study + Install-in-2-minutes + \$5 CTA. Screenshot → comment on EPIC #422.
2. **Sign-in flow** — `/account` magic-link arrives via Stalwart, NextAuth cookie set, redirect to `/welcome` (preferred_landing_role NULL).
3. **Persona pick** — `/welcome` shows three cards (Provider / Customer / VPN); click → PUT /api/v1/me/preferred-landing-role → router.push to that virtual app.
4. **Provider dashboard** — `/provider` renders AppShell (16px rail + 224px PROVIDER sidebar + content with paired-machines card + KPIs).
5. **Admin app** (post #426) — `admin.iogrid.org/` shows the brand-palette + left-rail (PR #446).

Until that walk lands + screenshot evidence comes back, EPIC #422 stays open.
