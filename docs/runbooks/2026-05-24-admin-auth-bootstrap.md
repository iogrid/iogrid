# 2026-05-24 — admin NextAuth bootstrap

> Per-incident playbook: when a fresh prov / fresh admin Deployment
> first comes up, NextAuth needs three Secrets in place before
> `/api/auth/session` returns HTTP 200. This walkthrough captures the
> exact commands so it never takes 4 distinct issues again
> (#475 + #476 in this session).

## TL;DR — one command

```bash
./scripts/admin-auth-bootstrap.sh
```

Idempotent. Mints AUTH_SECRET if absent, rotates admin_user
password each run, refreshes DATABASE_URL Secret, applies the
NextAuth Drizzle schema (`user`, `account`, `session`,
`verificationToken` — all `IF NOT EXISTS`), rolls the Deployment,
and verifies `/api/auth/session` returns 200 on a retry-budgeted
probe.

## TL;DR — manual (only if the script is unavailable)

```bash
# 1. AUTH_SECRET — random 32-byte NextAuth signing key.
SECRET=$(openssl rand -base64 32)
kubectl -n iogrid create secret generic admin-auth-secrets \
  --from-literal=AUTH_SECRET="$SECRET" \
  --dry-run=client -o yaml | kubectl apply -f -

# 2. admin DB schema + role in shared iogrid-pg CNPG cluster.
PASS=admin_$(openssl rand -hex 16)
kubectl -n iogrid exec iogrid-pg-1 -c postgres -- \
  psql -U postgres -c "CREATE DATABASE admin;"
kubectl -n iogrid exec iogrid-pg-1 -c postgres -- \
  psql -U postgres -c "CREATE USER admin_user WITH ENCRYPTED PASSWORD '$PASS';"
kubectl -n iogrid exec iogrid-pg-1 -c postgres -- \
  psql -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE admin TO admin_user;"

# 3. DATABASE_URL → DrizzleAdapter sees a non-null `db`, NextAuth
#    EmailProvider's MissingAdapter error goes away.
URL="postgres://admin_user:$PASS@iogrid-pg-rw.iogrid.svc.cluster.local:5432/admin?sslmode=require"
kubectl -n iogrid create secret generic admin-db-secrets \
  --from-literal=DATABASE_URL="$URL" \
  --dry-run=client -o yaml | kubectl apply -f -

# 4. Roll the Deployment so envFrom picks up the new Secrets.
kubectl -n iogrid rollout restart deploy/admin
```

`infra/k8s/base/admin/deployment.yaml` declares both Secrets in
`envFrom` with `optional: true` so the Deployment doesn't fail to
start when the Secrets don't exist — the binary surfaces the
MissingAdapter / MissingSecret error instead, which is easier to
debug.

`AUTH_TRUST_HOST=true` is hardcoded in the Deployment env (not in a
Secret) — it's a constant for any cluster behind a reverse proxy
that terminates TLS (Traefik does).

## Verification

```bash
for i in 1 2 3 4 5; do
  curl -sS -o /dev/null -w "[$i] HTTP %{http_code}\n" \
    https://admin.iogrid.org/api/auth/session
done
# All 5 should print HTTP 200.

kubectl -n iogrid logs deploy/admin --tail=20 | grep -iE 'error|warn'
# Should NOT show MissingSecret, MissingAdapter, or UntrustedHost.
```

## Failure modes

| Symptom | Cause | Fix |
|---|---|---|
| `MissingSecret` | AUTH_SECRET env not set | Step 1 above |
| `UntrustedHost` | AUTH_TRUST_HOST not set | Deployment env (hardcoded) |
| `MissingAdapter` | DATABASE_URL not set OR points at wrong db | Step 2-3 |
| `connect ECONNREFUSED` | `iogrid-pg-rw` Service not resolvable | Check `kubectl -n iogrid get svc iogrid-pg-rw` |
| Drizzle migration error | Schema not initialised | `cd admin && pnpm drizzle-kit push` against the new DB |

## Refs

- [#475](https://github.com/iogrid/iogrid/issues/475) AUTH_TRUST_HOST + AUTH_SECRET
- [#476](https://github.com/iogrid/iogrid/issues/476) DrizzleAdapter DATABASE_URL
- [#422](https://github.com/iogrid/iogrid/issues/422) parent EPIC — admin app separation
