# 2026-05-31 — Hatice provider_id cleanup runbook (post-#503)

> One-time data migration to bring `hatice.yildiz@openova.io` back onto the
> canonical provider id `808ce330-79c1-4390-8cc6-87c5ce5a94d8` after the
> drift documented in #502.
>
> **Pre-requisite**: PR #503 merged + Flux has rolled `providers-svc`
> with the new `GetProviderByOwnerAndPublicKey` lookup. Without #503 live
> on the coordinator, step 5 below would mint yet another fresh UUID.

## Why this is needed

PR #503 prevents FUTURE drift by switching the dedupe key from
`(owner_user_id, display_name)` to `(owner_user_id, public_key)`. It
does NOT undo the existing split: Hatice's daemon currently runs as
`cac83611-4a6f-4937-95b4-8f4fb2538808` (`provider-a7a93576` row),
while the canonical id used in test fixtures
(`web/src/test/provide-paired-machines.test.tsx`) and operator memory
is `808ce330-79c1-4390-8cc6-87c5ce5a94d8` (`Hatice's Mac` row,
currently deactivated).

This runbook merges cac83611's live state INTO the 808ce330 row,
re-points Hatice's daemon at the canonical id via a clean re-pair, and
deletes the cac83611 ghost.

## Baseline (captured 2026-05-31T10:35Z)

Hatice's Mac:
- `~/.iogrid/config.toml`: `provider_id = "cac83611-4a6f-4937-95b4-8f4fb2538808"`
- `~/.iogrid/cert.pem` subject CN = `cac83611-...`
- `~/.iogrid/key.pem` SPKI DER hex = `3059...7d8f4381b03284e5...18cdc50`
- `scutil --get LocalHostName` = `Hatices-Mac-mini-2` (Bonjour `-2` suffix LIVE)

Coordinator DB (`providers` table):

| id | display_name | spki_hex (suffix) | last_seen_at | is_primary | status |
|---|---|---|---|---|---|
| `808ce330-...87c5ce5a94d8` | `Hatice's Mac` | `...d1d860` | 2026-05-19 20:29:14 | f | deactivated |
| `cac83611-...8f4fb2538808` | `provider-a7a93576` | `...18cdc50` | 2026-05-31 10:32:38 | t | active |

Hatice's owner_user_id: `a7a93576-aebb-453e-bfc5-f9c31514e9da`.

## Procedure

### Step 0 — confirm PR #503 is live in the cluster

Run from the bastion:

```bash
kubectl -n iogrid rollout status deploy/providers-svc --timeout=300s
kubectl -n iogrid logs deploy/providers-svc --tail=200 | grep -i 'SPKI match' || echo 'no SPKI logs yet (no re-pair since rollout)'
```

If the image tag predates the merged commit, force a re-roll once Flux
catches up.

### Step 1 — snapshot the current row state

```bash
kubectl -n iogrid exec iogrid-pg-1 -- psql -U postgres -d providers -A -F'|' \
  -c "SELECT id, owner_user_id, display_name, encode(public_key,'hex') as spki_hex, registered_at, last_seen_at, is_primary, status FROM providers WHERE owner_user_id = 'a7a93576-aebb-453e-bfc5-f9c31514e9da';" \
  > /tmp/hatice-pre-cleanup.tsv
cat /tmp/hatice-pre-cleanup.tsv
```

Verify the two rows match the Baseline table above.

### Step 2 — migrate FK references from cac83611 → 808ce330

Audit events, scheduling configs, earnings entries — anything keyed by
`provider_id`. Run in ONE transaction:

```bash
kubectl -n iogrid exec iogrid-pg-1 -- psql -U postgres -d providers <<'SQL'
BEGIN;

-- Audit events
UPDATE provider_audit_events
   SET provider_id = '808ce330-79c1-4390-8cc6-87c5ce5a94d8'
 WHERE provider_id = 'cac83611-4a6f-4937-95b4-8f4fb2538808';

-- Scheduling config (ON CONFLICT collapse: cac83611's config wins if
-- both rows have one; otherwise just rebind the FK).
INSERT INTO scheduling_configs (provider_id, bandwidth_cap_gb, cpu_cap_pct, memory_cap_pct, idle_threshold_secs, idle_enabled, allowed_categories, disallowed_categories, destination_blocklist, per_customer_minutes_cap, updated_at, updated_by_user_id)
SELECT '808ce330-79c1-4390-8cc6-87c5ce5a94d8',
       bandwidth_cap_gb, cpu_cap_pct, memory_cap_pct, idle_threshold_secs,
       idle_enabled, allowed_categories, disallowed_categories,
       destination_blocklist, per_customer_minutes_cap, updated_at, updated_by_user_id
  FROM scheduling_configs
 WHERE provider_id = 'cac83611-4a6f-4937-95b4-8f4fb2538808'
ON CONFLICT (provider_id) DO UPDATE SET
  bandwidth_cap_gb       = EXCLUDED.bandwidth_cap_gb,
  cpu_cap_pct            = EXCLUDED.cpu_cap_pct,
  memory_cap_pct         = EXCLUDED.memory_cap_pct,
  idle_threshold_secs    = EXCLUDED.idle_threshold_secs,
  idle_enabled           = EXCLUDED.idle_enabled,
  allowed_categories     = EXCLUDED.allowed_categories,
  disallowed_categories  = EXCLUDED.disallowed_categories,
  destination_blocklist  = EXCLUDED.destination_blocklist,
  per_customer_minutes_cap = EXCLUDED.per_customer_minutes_cap,
  updated_at             = EXCLUDED.updated_at,
  updated_by_user_id     = EXCLUDED.updated_by_user_id;
DELETE FROM scheduling_configs WHERE provider_id = 'cac83611-4a6f-4937-95b4-8f4fb2538808';

-- Earnings (in billing-svc DB? check provider_id column there too).
-- iogrid keeps earnings per-service; this UPDATE only touches the
-- providers-DB local entries if present. The billing-svc table lives
-- separately under iogrid-billing-db.

COMMIT;
SQL
```

If the workloads-svc or billing-svc tables also key on `provider_id`,
mirror the rebinding there. Use:

```bash
kubectl -n iogrid get cluster.postgresql.cnpg.io iogrid-pg -o jsonpath='{.spec.bootstrap.initdb.postInitApplicationSQL}' | head -40
# Inspect schemas to confirm whether they reference providers.id.
```

### Step 3 — rebind the 808ce330 row to Hatice's CURRENT public_key + activate

This is the key swap: 808ce330's row gets Hatice's current
daemon-controlled keypair (which matches her on-disk `~/.iogrid/key.pem`).

The hex below MUST match the value captured in Step 1 for cac83611
(her active row). Replace if the captured hex differs.

```bash
HATICE_SPKI_HEX="3059301306072a8648ce3d020106082a8648ce3d030107034200047d8f4381b03284e5f1d87950505ce922feaa75db9e98b784970c862b92a2d64d77df4e04745c6a53f3b8a18b3e529c75b1b9dd04deebd649f601bb15618cdc50"

kubectl -n iogrid exec iogrid-pg-1 -- psql -U postgres -d providers <<SQL
BEGIN;
-- Move 808ce330 back to active with Hatice's current key + the live
-- hostname. last_seen will be refreshed on the next heartbeat tick.
UPDATE providers
   SET public_key       = decode('${HATICE_SPKI_HEX}', 'hex'),
       display_name     = 'Hatices-Mac-mini-2',
       status           = 'active',
       last_seen_at     = now(),
       is_primary       = false  -- flip to true AFTER cac83611 is_primary cleared
 WHERE id = '808ce330-79c1-4390-8cc6-87c5ce5a94d8';

-- Clear cac83611's primary flag BEFORE we promote 808ce330 (partial
-- unique index providers_primary_per_owner enforces at-most-one TRUE).
UPDATE providers SET is_primary = false
 WHERE id = 'cac83611-4a6f-4937-95b4-8f4fb2538808';

UPDATE providers SET is_primary = true
 WHERE id = '808ce330-79c1-4390-8cc6-87c5ce5a94d8';

-- Drop the orphan ghost.
DELETE FROM providers WHERE id = 'cac83611-4a6f-4937-95b4-8f4fb2538808';
COMMIT;
SQL
```

Verify:

```bash
kubectl -n iogrid exec iogrid-pg-1 -- psql -U postgres -d providers -A -F'|' \
  -c "SELECT id, display_name, encode(public_key,'hex') as spki, is_primary, status FROM providers WHERE owner_user_id = 'a7a93576-aebb-453e-bfc5-f9c31514e9da';"
```

Expect exactly one row, `id=808ce330-...`, `display_name=Hatices-Mac-mini-2`,
`spki` matching `HATICE_SPKI_HEX`, `is_primary=t`, `status=active`.

### Step 4 — issue a fresh pairing token via the admin API

```bash
# As emrah (global admin) on the bastion:
gh -R iogrid/iogrid auth status  # confirm operator scope
ADMIN_TOKEN=$(kubectl -n iogrid get secret iogrid-admin-token -o jsonpath='{.data.token}' | base64 -d)
PAIRING_TOKEN=$(curl -fsS -H "Authorization: Bearer ${ADMIN_TOKEN}" \
  -X POST "https://app.iogrid.org/api/v1/admin/providers/pairing-tokens" \
  -d '{"owner_user_id":"a7a93576-aebb-453e-bfc5-f9c31514e9da","ttl_seconds":600}' | jq -r .pairing_token)
echo "${PAIRING_TOKEN}"
```

(If a direct admin endpoint isn't wired, issue via the same flow Hatice
would use in the browser — sign in as her, hit /provide, copy the
6-char Crockford code.)

### Step 5 — drive `iogridd pair` on Hatice's Mac via the 2223 tunnel

```bash
ssh -i ~/.ssh/openova_migration openova@144.91.121.182 \
  "ssh -p 2223 -i ~/.ssh/claude_offload emrah@localhost \
    'iogridd pair ${PAIRING_TOKEN}'"
```

Expected daemon-side output:

```
paired: provider_id=808ce330-79c1-4390-8cc6-87c5ce5a94d8
```

What happened under the hood (post-#503):
1. Daemon loaded existing `~/.iogrid/key.pem` → CSR carries the
   `7d8f4381b032...` SPKI.
2. providers-svc's `PairDaemon` ran the SPKI lookup
   `(a7a93576-..., 7d8f4381b032...)` → matched the 808ce330 row we
   just rebound. UPDATE in place.
3. `display_name` refreshed to whatever the live hostname was.
4. ca issued a new cert with `CN=808ce330-79c1-4390-8cc6-87c5ce5a94d8`.
5. Daemon wrote `provider_id = "808ce330-..."` to `config.toml`,
   persisted new `cert.pem`. `key.pem` unchanged (same bytes as
   pre-pair, by the `load_or_mint_pairing_key` reuse path).

### Step 6 — confirm steady state on both sides

Daemon side:

```bash
ssh -i ~/.ssh/openova_migration openova@144.91.121.182 \
  "ssh -p 2223 -i ~/.ssh/claude_offload emrah@localhost '
    grep provider_id ~/.iogrid/config.toml
    openssl x509 -in ~/.iogrid/cert.pem -noout -subject
    iogridd diag --json | jq .config_summary.provider_id'"
```

Expected: `provider_id = "808ce330-..."` everywhere.

Coordinator side:

```bash
kubectl -n iogrid exec iogrid-pg-1 -- psql -U postgres -d providers -A -F'|' \
  -c "SELECT id, display_name, last_seen_at, is_primary, status FROM providers WHERE owner_user_id = 'a7a93576-aebb-453e-bfc5-f9c31514e9da';"
```

Expected: one row, `808ce330-...`, `last_seen_at` advancing (within
the heartbeat cadence), `is_primary=t`, `status=active`.

### Step 7 — drift simulation (proof the fix sticks)

```bash
ssh -i ~/.ssh/openova_migration openova@144.91.121.182 \
  "ssh -p 2223 -i ~/.ssh/claude_offload emrah@localhost '
    scutil --set LocalHostName Hatices-Mac-mini-RENAMED-SMOKE
    iogridd pair $(issue-another-pairing-token)
    grep provider_id ~/.iogrid/config.toml
    scutil --set LocalHostName Hatices-Mac-mini-2  # restore
   '"
```

Expected: `provider_id` remains `808ce330-...` because SPKI matches
regardless of the hostname rename. `display_name` on the row gets
refreshed to `Hatices-Mac-mini-RENAMED-SMOKE` then back to
`Hatices-Mac-mini-2` after the restore-pair. **NO new UUID minted.**

### Step 8 — close #502

Attach screenshots of `/admin/providers` showing the single
`808ce330` row + post-drift screenshot showing same UUID + refreshed
hostname. Then:

```bash
gh -R iogrid/iogrid issue close 502 --reason completed --comment 'verified — see runbook in docs/sessions/2026-05-31-hatice-provider-id-cleanup.md + comments above'
```

## Rollback

If anything in steps 2-3 looks wrong:

```sql
BEGIN;
-- Re-create cac83611 from the pre-cleanup snapshot in /tmp/hatice-pre-cleanup.tsv.
-- See pre-cleanup snapshot for exact byte values.
ROLLBACK;  -- only if you ran step 3 inside a transaction that's still open
```

The whole runbook is reversible from the Step 1 snapshot — keep
`/tmp/hatice-pre-cleanup.tsv` until Step 8 lands.
