# Runbooks

> **WHAT:** Operator how-tos for routine iogrid operations — Solana devnet bootstrap, status-page incident handling, etc.
> **AUTHORITY:** Canon for operator procedures. Per-incident playbooks (one-off historical incidents) live in [`runbooks/`](./runbooks/).
> **POINTER:** Architecture (the WHY) → [`ARCHITECTURE.md`](./ARCHITECTURE.md). Live verification ledger → [`ledger/TRUST.md`](./ledger/TRUST.md).

This document is the **canonical procedural catalog**. Each section is a self-contained runbook you can hand to a new operator.

---

## 1. Solana / $GRID — Phase 0 operator runbook

> Source: previously `docs/SOLANA.md` (merged here on 2026-05-21).

This is the founder/operator runbook for getting `billing-svc` out of stub mode by minting a Phase-0 **devnet** $GRID SPL token and wiring the resulting mint address + hot-wallet keypair into the Kubernetes Secret that `billing-svc` reads at boot.

> **Phase 0 = devnet only.** No real money moves. The mainnet TGE (Token Generation Event) is end-state work tracked in [EPIC #87](https://github.com/iogrid/iogrid/issues/87) and described in [BUSINESS-STRATEGY.md §4 (Currency model — $GRID + fiat hybrid)](./BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid).
>
> Mainnet wiring will replace the single-sig hot-wallet keypair below with a [Squads Protocol](https://squads.so) 2-of-3 multisig — see `coordinator/services/billing-svc/internal/solana/multisig.go`.
>
> **Current state (2026-06-03):** the devnet $GRID mint already exists —
> `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` (Token-2022, **9 decimals**,
> freeze-authority null), recorded in [`SOLANA-ADDRESSES.md`](./SOLANA-ADDRESSES.md).
> The **mainnet mint is NOT deployed** (founder decision; the mainnet address is
> TBD/empty). The steps below are the reproducible procedure for re-minting a
> fresh devnet token from scratch.

### 1.1 What this runbook gives you

When you finish:

- `billing-svc` log line on startup changes from
  `WARN solana: stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset)`
  to
  `INFO solana: live mode wallet=... mint=... token_program=...`.
- The in-process daily cron starts: `INFO solana: daily cron starting`.
- The cron's first tick runs the previous-day window. In Phase 0 the `usage_event` table is empty so `RunDailySwapAndDistribute` short-circuits at `grand == 0 → return nil`. **No on-chain calls happen** until the mothership starts ingesting metering events. This is the intended Phase 0 behaviour: code paths are exercised, logs prove the wiring, but the network sees zero TXs.

### 1.2 Prerequisites

A workstation with:

- `solana` CLI ≥ 1.18.x — install via `sh -c "$(curl -sSfL https://release.solana.com/stable/install)"`.
- `spl-token` CLI (ships with the Solana CLI tarball).
- `kubectl` pointed at the mothership cluster (the same kubeconfig used for the Phase 0 unblock procedure — see [`archive/2026-05-21-phase0-unblock.md`](./archive/2026-05-21-phase0-unblock.md)).
- Internet egress to `https://api.devnet.solana.com`.

### 1.3 Step-by-step

**Step 1 — generate the hot-wallet keypair.** The hot wallet is the single signer for all Phase 0 payouts + burns.

```bash
# Output the 64-byte keypair to a tmpfile. Do not check this into git.
solana-keygen new --no-bip39-passphrase --force \
  -o /tmp/iogrid-payout.json

# Verify the file looks right (array of 64 integers).
head -c 80 /tmp/iogrid-payout.json && echo
# expect: [12,34,56,...]

# Print the pubkey — needed for the airdrop + mint-authority below.
HOT_WALLET=$(solana-keygen pubkey /tmp/iogrid-payout.json)
echo "hot-wallet pubkey: $HOT_WALLET"
```

**Step 2 — fund the hot wallet on devnet.** Devnet SOL is free; airdrop the operating balance the wallet needs to pay rent (`spl-token create-token` costs ≈ 0.0014 SOL) and tx fees.

```bash
solana airdrop 5 "$HOT_WALLET" --url https://api.devnet.solana.com
solana balance "$HOT_WALLET" --url https://api.devnet.solana.com
# expect: 5 SOL
```

> If the public devnet faucet is rate-limited, retry with the per-request limit (`solana airdrop 1 …`) up to 5 times, or use [https://faucet.solana.com](https://faucet.solana.com).

**Step 3 — mint the devnet $GRID token.** `$GRID` is minted under the [Token-2022](https://spl.solana.com/token-2022) program in production (transfer hooks, metadata extensions). For Phase 0 we use Token-2022 too so the chainClient picks the right program-id without per-environment config drift.

```bash
MINT_OUTPUT=$(spl-token create-token \
  --program-id TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb \
  --url https://api.devnet.solana.com \
  --decimals 9 \
  --fee-payer /tmp/iogrid-payout.json \
  --mint-authority "$HOT_WALLET")

GRID_MINT=$(echo "$MINT_OUTPUT" | awk '/Creating token/ {print $3}')
echo "GRID devnet mint: $GRID_MINT"
```

**Save `$GRID_MINT`** — you'll paste it into the Kubernetes Secret in Step 5 and commit it to the public `docs/transparency/*.md` quarterly report so providers can verify the mint address out-of-band.

> The hot wallet is both the **mint authority** and the **freeze authority** in Phase 0. Mainnet TGE flips both to the Squads multisig.

**Step 4 (optional) — pre-mint $GRID supply for testing.** For a non-empty Jupiter quote round-trip on devnet you'll want some $GRID in the hot wallet's Associated Token Account. Skip this step if you only want the "log proves wiring" outcome.

```bash
spl-token create-account "$GRID_MINT" \
  --url https://api.devnet.solana.com \
  --fee-payer /tmp/iogrid-payout.json \
  --owner "$HOT_WALLET"

spl-token mint "$GRID_MINT" 1000000 \
  --url https://api.devnet.solana.com \
  --fee-payer /tmp/iogrid-payout.json \
  --mint-authority /tmp/iogrid-payout.json

spl-token balance "$GRID_MINT" \
  --url https://api.devnet.solana.com \
  --owner "$HOT_WALLET"
# expect: 1000000
```

**Step 5 — create the Kubernetes Secret.** The Secret carries two keys:

| Key                       | Type   | Read by billing-svc as                                          |
|---------------------------|--------|-----------------------------------------------------------------|
| `keypair_json`            | file   | `SOLANA_HOT_WALLET_KEYPAIR_PATH=/var/run/solana/keypair.json`   |
| `grid_token_mint_address` | string | env `GRID_TOKEN_MINT_ADDRESS`                                   |

```bash
# Confirm the Secret doesn't already exist (delete & recreate if so).
kubectl -n iogrid get secret iogrid-solana-payout 2>/dev/null \
  && kubectl -n iogrid delete secret iogrid-solana-payout

kubectl -n iogrid create secret generic iogrid-solana-payout \
  --from-file=keypair_json=/tmp/iogrid-payout.json \
  --from-literal=grid_token_mint_address="$GRID_MINT"

# Wipe the local keypair file.
shred -u /tmp/iogrid-payout.json

# Verify both keys are present.
kubectl -n iogrid get secret iogrid-solana-payout \
  -o jsonpath='{.data}' | jq 'keys'
# expect: ["grid_token_mint_address", "keypair_json"]
```

**Step 6 — roll billing-svc + verify the logs.**

```bash
kubectl -n iogrid rollout restart deploy/billing-svc
kubectl -n iogrid rollout status  deploy/billing-svc --timeout=2m
kubectl -n iogrid logs deploy/billing-svc --since=2m | grep -iE 'solana|cron'
```

Expected output:

```
INFO solana: live mode wallet=8xQ4...e3rT mint=GRId...xN9w token_program=TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb multisig_mode=single-sig
INFO solana: daily cron starting schedule="5 0 * * *" live=true
INFO solana daily loop: no revenue for window start=2026-05-19T00:00:00Z
```

If you instead see `WARN solana: stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset)`, the Secret was not picked up. Re-check:

```bash
kubectl -n iogrid get pod -l app.kubernetes.io/name=billing-svc \
  -o jsonpath='{.items[0].spec.containers[0].env}' | jq
# expect a GRID_TOKEN_MINT_ADDRESS entry with valueFrom.secretKeyRef.name=iogrid-solana-payout

kubectl -n iogrid exec deploy/billing-svc -- ls -l /var/run/solana
# expect: -r-------- 1 65532 65532 ... keypair.json
```

**Step 7 — bind providers to the devnet wallet.** For Phase 0 we don't run real payouts on devnet — but providers should still see their wallet address in the dashboard. `identity-svc` exposes a Solana wallet-bind endpoint (Sign-In-With-Solana). The web UI is tracked in EPIC #87.

If you want to exercise the swap path end-to-end on devnet, fund a few provider pubkeys with airdropped SOL, bind them via the wallet-connect button on `iogrid.org` (the apex serves the app — `app.iogrid.org` was dropped per EPIC #422), then insert a synthetic `usage_event` row so the cron has something to distribute against:

```sql
-- Connect to the iogrid Postgres (kubectl port-forward svc/iogrid-pg-rw 5432).
INSERT INTO usage_event (id, provider_id, customer_id, kind, amount_cents, started_at, ended_at)
VALUES (gen_random_uuid(), '<PROVIDER_UUID>', '<CUSTOMER_UUID>', 'PROXY_REQ', 100, now() - interval '1 day', now() - interval '1 day');
```

Then trigger the cron manually by restarting billing-svc (the on-boot run-for-yesterday tick fires every Pod start). Tail the logs:

```bash
kubectl -n iogrid logs deploy/billing-svc -f | grep -iE 'jupiter|payout|burn|swap'
```

Expected lines (devnet Jupiter quote, may 404 if the LP pool doesn't exist on devnet — that's fine, the row transitions to FAILED and is retried on the next tick):

```
INFO solana: quoting jupiter outputMint=GRId...xN9w usd_cents=100
INFO solana: tx submitted signature=...
INFO solana: payout confirmed provider_id=... amount_lamports=...
```

### 1.4 Security notes

- **Never commit `/tmp/iogrid-payout.json` or its contents.** This repo is public. The Secret object stays in-cluster; the only place the 64-byte keypair touches disk on the cluster is the projected file at `/var/run/solana/keypair.json` (mode 0400, owner 65532, container filesystem is read-only).
- The hot wallet's pubkey is fine to share publicly (it's how anyone verifies on-chain activity). The `keypair_json` payload is the full ed25519 keypair = full spend authority.
- Rotate the hot wallet by repeating Steps 1–6 (the new keypair replaces the old one in the Secret; old funds need to be swept to the new pubkey before rotation).

### 1.5 Mainnet flip

When the iogrid Foundation is ready for the TGE:

1. Create the production mint under Token-2022 with the **Squads multisig** as both mint and freeze authority (NOT a single-sig keypair).
2. Set `SQUADS_MULTISIG_PUBKEY` in `billing-svc-secrets` — the Go layer detects this and routes every write through the Squads vault (see `internal/solana/multisig.go`).
3. Replace `SOLANA_RPC_URL` in `billing-svc-config` from `https://api.devnet.solana.com` to a paid mainnet RPC (Helius / Triton / your own).
4. Set `BURN_VIA_INCINERATOR=false` so the daily burn uses a real `BurnChecked` instruction (cheaper, on-chain accounting is exact).
5. Audit the mint authority + freeze authority addresses match the multisig's vault PDA before anyone sends real funds.

The Go layer needs **no code changes** between devnet and mainnet — the entire diff is operator-provided configuration.

---

## 2. Status page operator runbook

> Source: previously `docs/RUNBOOK_STATUS.md` (merged here on 2026-05-21).

The public status page lives at **status.iogrid.org** (served by the web app's `/status/` route). Its data plane is the **telemetry-svc** microservice running in the iogrid coordinator namespace. This runbook explains how to operate it during an outage.

> If you're reading this on the bastion and you need to act fast — the four commands you most likely want are in the [TL;DR cheat sheet](#27-tldr) at the bottom.

### 2.1 Architecture (one page)

```
  ┌─────────────────────┐                       ┌──────────────────────┐
  │ status.iogrid.org   │   GET /status/posture │ telemetry-svc        │
  │ (web app /status)   │ ────────────────────▶ │  /status             │
  │                     │                       │  /status/posture     │
  │  ↻ every 60s        │   GET /status/uptime  │  /status/uptime      │
  └─────────────────────┘ ◀──────────────────── │  /status/subscribe   │
                          POST /status/subscribe│  /status/incidents   │
                                                 └──────────┬───────────┘
                                                            │ pgx
                                                            ▼
                                                   ┌──────────────────┐
                                                   │  CNPG Postgres   │
                                                   │  incidents/      │
                                                   │  subscriptions/  │
                                                   │  uptime_samples  │
                                                   └──────────────────┘
```

- Public reads (`/status`, `/status/posture`, `/status/uptime`) are unauthenticated, world-readable, cached at 30s.
- Mutations (`POST /status/incidents`, `POST /status/incidents/{id}/updates`) require `Authorization: Bearer $ADMIN_TOKEN` — the shared admin token lives in `Secret/telemetry-svc-admin` in the `iogrid` namespace.
- `POST /status/subscribe` is public but rate-limited per-IP (60 req / minute).

### 2.2 During an outage: manually create an incident

```bash
# 1. Resolve the admin endpoint. From the bastion, telemetry-svc is
#    reachable through the in-cluster service:
ADMIN=$(kubectl -n iogrid get secret telemetry-svc-admin \
  -o jsonpath='{.data.token}' | base64 -d)

# 2. Open a port-forward — avoids exposing the admin path on the public ingress.
kubectl -n iogrid port-forward svc/telemetry-svc 8088:80 &
PF_PID=$!
trap "kill $PF_PID" EXIT

# 3. POST the incident.
curl -sS -X POST http://localhost:8088/status/incidents \
  -H "Authorization: Bearer $ADMIN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Regional proxy-gateway outage (eu-central-1)",
    "body":  "We are investigating elevated 5xx on the bandwidth proxy in eu-central-1. Other regions unaffected.",
    "status": "investigating",
    "impact": "critical",
    "affected_services": ["proxy-gateway"]
  }' | jq .
```

Within ~30 seconds the public status page will show the new headline banner ("Major outage") and the incident card.

### 2.3 Posting an update

```bash
INCIDENT_ID=<paste id from previous response>

curl -sS -X POST http://localhost:8088/status/incidents/${INCIDENT_ID}/updates \
  -H "Authorization: Bearer $ADMIN" \
  -H "Content-Type: application/json" \
  -d '{
    "status": "identified",
    "body": "Root cause: upstream Hetzner Network outage in eu-central-1. Rerouting traffic via eu-west-1 / us-east-1."
  }' | jq .
```

Status transition rules:

| From → To        | What changes |
|------------------|--------------|
| `investigating → identified` | Headline stays the same; update appears |
| `identified → monitoring`    | Headline stays the same; status pill updates |
| `monitoring → resolved`      | `resolved_at` stamped; banner colour recovers if no other active incident |

The same endpoint handles "manual rollback" — POST a `resolved` update with a brief postmortem line.

### 2.4 Triggering an uptime backfill

The 90-day heatmap is fed by a synthetic worker that records ONE `(service, day)` row per day at UTC midnight. If the worker missed a day (Pod crashloop, mothership query disruption), backfill manually:

```bash
kubectl -n iogrid exec -it cnpg-iogrid-1 -- psql -U telemetry telemetry

-- Insert / overwrite one day:
INSERT INTO uptime_samples (service, day, state, sli_pct)
VALUES ('proxy-gateway', '2026-05-15', 'down', 87.40)
ON CONFLICT (service, day) DO UPDATE
  SET state = EXCLUDED.state, sli_pct = EXCLUDED.sli_pct;
```

Or query Mimir directly and replay:

```bash
for d in $(seq 0 6); do
  DAY=$(date -d "${d} days ago" +%Y-%m-%d)
  for SVC in proxy-gateway build-gateway identity-svc workloads-svc billing-svc vpn-gateway; do
    SLI=$(curl -fsS "${MIMIR_URL}/api/v1/query?query=slo:availability:30d{service=\"${SVC}\"}" \
      -H "X-Scope-OrgID: iogrid" -u "${MIMIR_BASIC_AUTH}" | jq -r '.data.result[0].value[1]')
    STATE=op
    awk -v s="$SLI" 'BEGIN { exit !(s+0 < 0.99) }' && STATE=deg
    awk -v s="$SLI" 'BEGIN { exit !(s+0 < 0.95) }' && STATE=down
    psql ... -c "INSERT INTO uptime_samples VALUES ('${SVC}','${DAY}','${STATE}',${SLI}*100)
                 ON CONFLICT (service, day) DO UPDATE
                 SET state=EXCLUDED.state, sli_pct=EXCLUDED.sli_pct;"
  done
done
```

For a planned-maintenance window, record the day as `maint` instead (renders blue on the heatmap, doesn't penalise the rolling uptime percentage):

```sql
INSERT INTO uptime_samples (service, day, state, sli_pct)
VALUES ('build-gateway', '2026-05-20', 'maint', 100)
ON CONFLICT (service, day) DO UPDATE
  SET state = EXCLUDED.state;
```

### 2.5 Subscription routing

The `/status/subscribe` endpoint inserts a row into `status_subscriptions`. It does NOT itself send an email — a separate notification worker reads the table and fans out via the Stalwart SMTP running on `mail.openova.io`.

To view current subscribers:

```sql
SELECT email, verified, created_at, services_filter
FROM status_subscriptions
WHERE unsubscribed_at IS NULL
ORDER BY created_at DESC
LIMIT 50;
```

To unsubscribe someone manually (GDPR / opt-out request):

```sql
UPDATE status_subscriptions
SET unsubscribed_at = now()
WHERE LOWER(email) = LOWER('user@example.com');
```

To re-trigger the verification email for an unverified subscriber:

```sql
UPDATE status_subscriptions
SET verify_token = gen_random_uuid()::text, created_at = now()
WHERE LOWER(email) = LOWER('user@example.com') AND verified = false;
```

### 2.6 When the status page itself is broken

The web app's `/status/` route ships a baseline "all systems operational" frame compiled from a bundled `incidents-static.json`. If `/status/posture` is unreachable, the page falls back to that frame and shows a "stale data" pill.

If the page itself is broken (white screen) — verify:

```bash
# 1. The static-export artifact exists.
curl -fsS https://status.iogrid.org/ | head -30

# 2. CORS allow-origin is set on telemetry-svc.
curl -i https://api.iogrid.org/status/posture | grep -i 'access-control'

# 3. The marketing-ci workflow's last run is green.
gh run list --workflow=marketing-ci -L 5
```

If `/status/posture` is reachable but returns stale data, look at the incident store backend:

```bash
kubectl -n iogrid logs deploy/telemetry-svc | grep -i incident
# Should see: "incident store wired (postgres)" — if you see
# "incident store wired (in-memory)" the DATABASE_URL Secret was not
# mounted, fix the SealedSecret first.
```

### 2.7 TL;DR

```bash
# Create incident (during outage):
curl -X POST http://localhost:8088/status/incidents \
  -H "Authorization: Bearer $ADMIN" \
  -H "Content-Type: application/json" \
  -d '{"title":"X","impact":"major","affected_services":["proxy-gateway"]}'

# Post update:
curl -X POST http://localhost:8088/status/incidents/$ID/updates \
  -H "Authorization: Bearer $ADMIN" \
  -d '{"status":"monitoring","body":"Mitigation deployed."}'

# Resolve:
curl -X POST http://localhost:8088/status/incidents/$ID/updates \
  -H "Authorization: Bearer $ADMIN" \
  -d '{"status":"resolved","body":"Service fully restored."}'

# Subscribe routing check:
psql -c "SELECT count(*) FROM status_subscriptions WHERE verified AND unsubscribed_at IS NULL;"
```

---

## 3. providers-svc — dedupe legacy duplicate provider rows

> **Scope:** one-off SQL cleanup against the providers-svc Postgres
> database after the daemon hostname / re-pair-dedupe fix lands (Refs #327).
> All operations are **owner-scoped**; no row is deleted unless the same
> owner still has another row that the daemon will heartbeat into.

### 3.1 Background

Before the daemon shipped OS-hostname as `display_name` + the
coordinator deduped on (`owner_user_id`, `display_name`), every daemon
reinstall on the same machine inserted a NEW row in `providers` (fresh
UUID, fresh public key, fresh display_name like
`provider-a7a93576-…`). Operators saw two or more rows per host in
`/admin/providers` — Hatice's Mac appeared as both `Hatice's Mac`
(legacy, never re-heartbeated) and `provider-a7a93576-…` (the live row).

After the fix, every fresh pair from the same host UPDATEs the existing
(owner, hostname) row — but pre-existing duplicates need a one-off
cleanup.

### 3.2 Pre-flight (READ-ONLY)

Inventory the candidate duplicates first. Never run the DELETE without
seeing the rows you're about to remove.

```sql
-- Owners that have BOTH an auto-generated `provider-…` row AND a
-- human-named row. These are the dedupe candidates.
WITH dup_owners AS (
  SELECT owner_user_id
    FROM providers
   GROUP BY owner_user_id
  HAVING bool_or(display_name LIKE 'provider-%')
     AND bool_or(display_name NOT LIKE 'provider-%')
)
SELECT id, owner_user_id, display_name, registered_at, last_seen_at, is_primary
  FROM providers
 WHERE owner_user_id IN (SELECT owner_user_id FROM dup_owners)
 ORDER BY owner_user_id, registered_at;
```

For each owner, decide manually:
- The row with the human-friendly name + most recent `last_seen_at` is
  the keeper.
- The `provider-…` row with the stale `last_seen_at` is safe to drop.
- If BOTH look stale (no daemon connected for >24h), do nothing — let
  the next pair re-dedupe.

### 3.3 Cleanup (DESTRUCTIVE — paste into a transaction first)

```sql
BEGIN;

-- Safety-net: only delete `provider-<short-id>` rows whose owner ALSO
-- has at least one human-named row that the new dedupe path will
-- reconverge on. NEVER deletes a row whose owner has zero replacement.
DELETE FROM providers p
 WHERE p.display_name LIKE 'provider-%'
   AND p.is_primary = FALSE
   AND EXISTS (
     SELECT 1 FROM providers q
      WHERE q.owner_user_id = p.owner_user_id
        AND q.id <> p.id
        AND q.display_name NOT LIKE 'provider-%'
   );

-- Inspect the row-count. Commit only if it matches your pre-flight
-- inventory. ROLLBACK otherwise.
-- COMMIT;
-- ROLLBACK;
```

### 3.4 Audit-trail (POST-CLEANUP)

```sql
-- After commit, re-run §3.2 — the result set MUST be empty (or contain
-- only owners with two human-named rows that legitimately represent
-- two machines).

-- Confirm no row was wrongly dropped: per-owner row counts.
SELECT owner_user_id, COUNT(*) AS rows_remaining
  FROM providers
 GROUP BY owner_user_id
 ORDER BY rows_remaining DESC
 LIMIT 20;
```

### 3.5 Notes

- The cleanup is **not** automated as a migration. Per founder rule
  (auto-DELETE on schema-migration could lose evidence of fraud /
  anti-abuse cases), one-off cleanups stay manual.
- The dedupe lookup in the handler is keyed on
  (`owner_user_id`, `display_name`) — owners with two genuinely distinct
  machines that happen to share a hostname (`localhost`,
  `Macbook-Pro.local`) will still collide. Recommend operators set a
  unique hostname per machine; daemon strips `.local` / `.lan` suffixes
  but does not deduplicate beyond that.

---

## 4. iogrid.org DNS — Dynadot apply runbook

> **Trigger:** any time you edit `infra/dynadot/iogrid-org-records.json` (add a subdomain, change an IP, etc.). Merging the PR is **not** sufficient — Dynadot is not GitOps-reconciled and the script must be run manually after merge.

### 4.1 Why this exists

iogrid's authoritative DNS lives at Dynadot. Dynadot's `set_dns2` API requires the **full** desired record set in one call (anything not in the call is removed from the zone). The source of truth is `infra/dynadot/iogrid-org-records.json`; `scripts/dynadot-apply.sh` is the **only** supported writer. Never hand-edit the Dynadot web UI — drift will be silently overwritten on the next apply.

### 4.2 The trap that caused #410

PR #408 added `admin.iogrid.org` to:
- `infra/k8s/certificates/iogrid-org-cert.yaml` (cert SAN list)
- `infra/k8s/traefik/ingressroute-admin.yaml` (Traefik route)
- `infra/dynadot/iogrid-org-records.json` (DNS source of truth)

It did **not** run `scripts/dynadot-apply.sh --apply`. Flux reconciled the K8s manifests, cert-manager opened an ACME Order with `admin.iogrid.org` in the SAN list, Let's Encrypt's HTTP-01 self-check failed with `no valid A records found for admin.iogrid.org`, and the entire Order was marked `invalid`. The `iogrid-org-tls` Secret was never created, so Traefik's TLS handshake for `iogrid.org` fell back to its built-in self-signed default cert — for the apex, not just `admin`. One missing API call broke TLS on the apex.

**Lesson:** any PR that touches `infra/dynadot/*.json` MUST be followed by a manual apply from a mothership shell **before** the related cert/IngressRoute change can complete. Treat the JSON file as a desired-state record but execution is still manual until Dynadot is wrapped in a Crossplane provider.

### 4.3 Step-by-step apply

```bash
# 1. Dry-run from anywhere — prints the set_dns2 URL it would call,
#    redacts the API key. Sanity-check the subdomain list.
./scripts/dynadot-apply.sh

# 2. Apply via a mothership pod (the mothership Contabo IP is allowlisted
#    on Dynadot's API; the bastion is not — so the script kubectl-runs a
#    one-shot alpine pod which inherits the mothership node's outbound
#    IP). Reads creds from secret/dynadot-api-credentials in openova-system.
./scripts/dynadot-apply.sh --apply

# 3. Verify globally (dig against 1.1.1.1, fails fast on any missing /
#    drifted record — the verify block is now derived from the JSON so
#    new subdomains are exercised automatically).
./scripts/dynadot-apply.sh --verify
```

Authoritative-NS lag is typically <2 min, public-resolver TTL is 300s.

### 4.4 What to do if cert-manager has already failed an Order

cert-manager will **not** retry a failed Order automatically — Let's Encrypt rate-limits aggressively. After fixing the DNS, force a retry by deleting the failed CertificateRequest:

```bash
kubectl -n iogrid get certificaterequest                  # find the failed CR (READY=False)
kubectl -n iogrid delete certificaterequest <cr-name>     # cascades the failed Order + Challenges
# cert-manager re-creates the CR + Order + Challenges within ~10s
kubectl -n iogrid get certificate iogrid-org-tls -w       # watch READY flip to True
```

Verify the live TLS cert is now from Let's Encrypt R12 (not Traefik default):

```bash
echo Q | openssl s_client -connect iogrid.org:443 -servername iogrid.org 2>/dev/null | \
  openssl x509 -noout -issuer -subject -ext subjectAltName
# Expect: issuer=O=Let's Encrypt, CN=R12
```

If Traefik continues serving the self-signed cert after the Secret materialises, restart it:

```bash
kubectl -n kube-system rollout restart deploy/traefik
```

(Traefik watches Secrets via the K8s informer, so a restart is rarely needed — but it's the standard last resort if the new Secret arrived during a controller hiccup.)

---

## 5. Coordinator env-var contract — canonical names for cross-service config

> **Source:** issue #416 (env-var name drift fix). Authority: this section is the canonical reference for every coordinator service + the Next.js web that talks to it. Any new spelling must be added here in the same PR that introduces it.

### Why this exists

A coordinator service that reads `os.Getenv("FOO_URL")` and gets back the empty string will boot, log nothing, and silently POST to `""`. The CronJob alerting sees `exit 0` because the binary did "complete". Meanwhile every transparency report is dropped on the floor. This is exactly the dormant-bug guard smell called out in [`~/.claude/CLAUDE.md` §3 anti-pattern catalog (service-name-mismatch in env-var defaults)](https://github.com/openova-io/openova/blob/main/CLAUDE.md).

The cure is a **canonical name per logical config item**, documented here, and a **fail-fast at startup** in any service whose runtime correctness depends on a non-empty value.

### Canonical names

| Logical config | Canonical env var | Scope | Notes |
|---|---|---|---|
| gateway-bff base URL (server-side dial) | `IOGRID_GATEWAY_BFF_URL` | Next.js Route Handlers, coordinator services that publish through gateway-bff | Default in-cluster value: `http://gateway-bff.iogrid.svc.cluster.local:8080`. |
| gateway-bff base URL (browser bundle) | `NEXT_PUBLIC_GATEWAY_URL` | Browser-side `ApiClient` + the `/provide/audit` SSE feed only | Next.js requires the `NEXT_PUBLIC_` prefix to expose any env var to the browser bundle. This is the **only** legal browser-visible spelling — it is the browser-bundle mirror of `IOGRID_GATEWAY_BFF_URL`. Leave it unset in production so all `/api/v1/*` traffic stays same-origin (the BFF proxy bridges to gateway-bff). |
| Shared inter-service bearer token | `IOGRID_SERVICE_TOKEN` | Next.js Route Handlers, gateway-bff auth middleware, identity-svc auth middleware, antiabuse-svc transparency-report | Sealed-Secret material; same value mounted in every pod that calls another iogrid service with the `Authorization: Bearer <tok>` + `X-Iogrid-User-Id` shim. |
| identity-svc base URL (server-side dial) | `IOGRID_IDENTITY_SVC_URL` | Next.js Route Handlers that need workspace ops | Falls back to `IOGRID_GATEWAY_BFF_URL` when unset (because gateway-bff proxies identity RPCs in Phase 0). |
| providers-svc base URL (server-side dial) | `IOGRID_PROVIDERS_RPC_URL` | `/api/v1/admin/providers/list` only | Optional override; defaults to `IOGRID_GATEWAY_BFF_URL`. |
| Documentation site base URL | `IOGRID_DOCS_BASE_URL` | `telemetry-svc` (alert `runbook_url` annotations), `gateway-bff` (customer onboarding JSON response), every future operator-facing docs link | Default: `https://docs.iogrid.org`. Private Sovereigns set this to their mirrored docs site (e.g. `https://docs.mygrid.example`) so alert payloads + onboarding responses do not leak the public URL. Read via `coordinator/shared/config.RunbookURL(slug)` / `config.DocsURL(...)` — **never** with hand-rolled `fmt.Sprintf("https://docs.iogrid.org/…", …)`. Refs #418. **Caveat:** alert `runbook_url` values are baked into the committed `infra/k8s/base/telemetry-svc/prometheusrules.yaml` at chart-build time, NOT read by the live pod, so a private Sovereign must re-run the generator with the env var set: `cd coordinator/services/telemetry-svc && IOGRID_DOCS_BASE_URL=https://docs.mygrid.example go run -tags=genrules ./scripts/gen-prometheus-rules.go > ../../../infra/k8s/base/telemetry-svc/prometheusrules.yaml`. |

### Retired spellings (removed in #416)

| Pre-#416 spelling | Replace with | Where it was |
|---|---|---|
| `GATEWAY_BFF_URL` | `IOGRID_GATEWAY_BFF_URL` | antiabuse-svc transparency-report Go binary |
| `GATEWAY_BFF_TOKEN` | `IOGRID_SERVICE_TOKEN` | antiabuse-svc transparency-report Go binary + the CronJob env block |
| `IOGRID_BFF_SERVICE_TOKEN` | `IOGRID_SERVICE_TOKEN` | `web/src/app/api/onboard/{start,complete}/route.ts` |

The transparency-report binary now **fails-fast at startup with exit 2** if `IOGRID_GATEWAY_BFF_URL` or `IOGRID_SERVICE_TOKEN` is empty (unless `-dry-run` is set). Exit 2 is distinct from exit 1 ("report generation failed at runtime") so the CronJob alert classifier can route "operator mis-wired the manifest" to the on-call ops channel rather than the generic transient-failure channel.

### Adding a new env var

Adding a new operator-facing env var to any coordinator service or to the Next.js web:

1. Pick a name with the `IOGRID_` prefix (or `NEXT_PUBLIC_` if it must reach the browser bundle).
2. Add a row to the canonical-names table above in the same PR.
3. Read it through a typed config struct in Go (`internal/config/config.go`) or a single helper in TS — never re-read with `os.Getenv` / `process.env` at multiple call sites with different default values.
4. If correctness depends on a non-empty value, fail-fast at startup. **No silent empty-string short-circuits.**
5. Add a sealed-Secret mount in `infra/k8s/base/<svc>/deployment.yaml` (or values block) using the canonical name.
