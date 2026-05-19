# Status page runbook

The public status page lives at **status.iogrid.org** (served by the
marketing site under `/status/`). Its data plane is the
**telemetry-svc** microservice running in the iogrid coordinator
namespace. This runbook explains how to operate it during an outage.

> If you're reading this on the bastion and you need to act fast —
> the four commands you most likely want are in the
> [TL;DR cheat sheet](#tldr) at the bottom.

---

## Architecture (one page)

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

- Public reads (`/status`, `/status/posture`, `/status/uptime`) are
  unauthenticated, world-readable, cached at 30s.
- Mutations (`POST /status/incidents`, `POST /status/incidents/{id}/updates`)
  require `Authorization: Bearer $ADMIN_TOKEN` — the shared admin
  token lives in `Secret/telemetry-svc-admin` in the `iogrid`
  namespace.
- `POST /status/subscribe` is public but rate-limited per-IP (60 req /
  minute).

---

## During an outage: manually create an incident

```bash
# 1. Resolve the admin endpoint. From the bastion, telemetry-svc is
#    reachable through the in-cluster service:
ADMIN=$(kubectl -n iogrid get secret telemetry-svc-admin \
  -o jsonpath='{.data.token}' | base64 -d)

# 2. Open a port-forward — this avoids exposing the admin path on the
#    public ingress.
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

Within ~30 seconds the public status page will show the new headline
banner ("Major outage") and the incident card.

## Posting an update

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

The same endpoint handles "manual rollback" — POST a `resolved` update
with a brief postmortem line.

---

## Triggering an uptime backfill

The 90-day heatmap is fed by a synthetic worker that records ONE
`(service, day)` row per day at UTC midnight. If the worker missed a
day (Pod crashloop, mothership query disruption), backfill manually:

```bash
# Connect to the telemetry-svc Postgres.
kubectl -n iogrid exec -it cnpg-iogrid-1 -- psql -U telemetry telemetry

-- Insert / overwrite one day:
INSERT INTO uptime_samples (service, day, state, sli_pct)
VALUES ('proxy-gateway', '2026-05-15', 'down', 87.40)
ON CONFLICT (service, day) DO UPDATE
  SET state = EXCLUDED.state, sli_pct = EXCLUDED.sli_pct;
```

Or query Mimir directly and replay:

```bash
# Pull the per-day SLI for each service from the slo:burn_rate:long
# recording rule. Substitute the cutoff dates and your Mimir creds.
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

For a planned-maintenance window, record the day as `maint` instead
(it renders blue on the heatmap, doesn't penalise the rolling
uptime percentage):

```sql
INSERT INTO uptime_samples (service, day, state, sli_pct)
VALUES ('build-gateway', '2026-05-20', 'maint', 100)
ON CONFLICT (service, day) DO UPDATE
  SET state = EXCLUDED.state;
```

---

## Subscription routing

The `/status/subscribe` endpoint inserts a row into
`status_subscriptions`. It does NOT itself send an email — a separate
notification worker reads the table and fans out via the Stalwart SMTP
running on `mail.openova.io`.

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
-- Bump the verify token and let the notification worker pick it up.
UPDATE status_subscriptions
SET verify_token = gen_random_uuid()::text, created_at = now()
WHERE LOWER(email) = LOWER('user@example.com') AND verified = false;
```

---

## When the status page itself is broken

The static export under `marketing/out/status/` includes a baseline
"all systems operational" frame compiled from
`marketing/content/status/incidents-static.json`. If
`/status/posture` is unreachable, the page falls back to that frame
and shows a "stale data" pill.

If the page itself is broken (white screen) — verify:

```bash
# 1. The static-export artifact exists.
curl -fsS https://status.iogrid.org/ | head -30

# 2. CORS allow-origin is set on telemetry-svc.
curl -i https://api.iogrid.org/status/posture | grep -i 'access-control'

# 3. The marketing-ci workflow's last run is green.
gh run list --workflow=marketing-ci -L 5
```

If `/status/posture` is reachable but returns stale data, look at the
incident store backend:

```bash
kubectl -n iogrid logs deploy/telemetry-svc | grep -i incident
# Should see: "incident store wired (postgres)" — if you see
# "incident store wired (in-memory)" the DATABASE_URL Secret was not
# mounted, fix the SealedSecret first.
```

---

## TL;DR

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
