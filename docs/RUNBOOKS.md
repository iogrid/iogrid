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

If you want to exercise the swap path end-to-end on devnet, fund a few provider pubkeys with airdropped SOL, bind them via the wallet-connect button on `app.iogrid.org`, then insert a synthetic `usage_event` row so the cron has something to distribute against:

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

The public status page lives at **status.iogrid.org** (served by the marketing site under `/status/`). Its data plane is the **telemetry-svc** microservice running in the iogrid coordinator namespace. This runbook explains how to operate it during an outage.

> If you're reading this on the bastion and you need to act fast — the four commands you most likely want are in the [TL;DR cheat sheet](#27-tldr) at the bottom.

### 2.1 Architecture (one page)

```
  ┌─────────────────────┐                       ┌──────────────────────┐
  │ status.iogrid.org   │   GET /status/posture │ telemetry-svc        │
  │ (static export, NX) │ ────────────────────▶ │  /status             │
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

The static export under `marketing/out/status/` includes a baseline "all systems operational" frame compiled from `marketing/content/status/incidents-static.json`. If `/status/posture` is unreachable, the page falls back to that frame and shows a "stale data" pill.

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

