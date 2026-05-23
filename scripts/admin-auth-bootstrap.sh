#!/usr/bin/env bash
# admin-auth-bootstrap.sh — idempotent admin NextAuth bootstrap.
#
# Mints the three things admin/ needs to serve /api/auth/session 200:
#   1. admin-auth-secrets.AUTH_SECRET — random 32-byte NextAuth signer
#   2. admin DB schema + admin_user role in shared iogrid-pg cluster
#   3. admin-db-secrets.DATABASE_URL → DrizzleAdapter
#
# Re-runnable: skips DB create if `admin` already exists, skips Secret
# create if already present + non-empty. Rolls the Deployment at the
# end to pick up env changes.
#
# Captures the #475 + #476 fix path so the next prov takes one command.

set -euo pipefail

NS=iogrid
PG_POD=iogrid-pg-1
PG_CONTAINER=postgres

ensure_secret_with_random() {
  local name=$1 key=$2 generator=$3
  if kubectl -n "$NS" get secret "$name" >/dev/null 2>&1; then
    local existing
    existing=$(kubectl -n "$NS" get secret "$name" \
                 -o jsonpath="{.data.$key}" 2>/dev/null)
    if [ -n "$existing" ]; then
      echo "  ✓ Secret/$name carries $key — keeping"
      return
    fi
  fi
  local value
  value=$(eval "$generator")
  kubectl -n "$NS" create secret generic "$name" \
    --from-literal="$key=$value" \
    --dry-run=client -o yaml | kubectl apply -f -
  echo "  ✓ Secret/$name minted with fresh $key"
}

ensure_db_exists() {
  if kubectl -n "$NS" exec "$PG_POD" -c "$PG_CONTAINER" -- \
       psql -U postgres -tAc "SELECT 1 FROM pg_database WHERE datname='admin';" \
       2>/dev/null | grep -q 1; then
    echo "  ✓ Database admin already exists"
    return
  fi
  kubectl -n "$NS" exec "$PG_POD" -c "$PG_CONTAINER" -- \
    psql -U postgres -c "CREATE DATABASE admin;"
  echo "  ✓ Database admin created"
}

ensure_user_with_password() {
  local pass=$1
  if kubectl -n "$NS" exec "$PG_POD" -c "$PG_CONTAINER" -- \
       psql -U postgres -tAc "SELECT 1 FROM pg_user WHERE usename='admin_user';" \
       2>/dev/null | grep -q 1; then
    kubectl -n "$NS" exec "$PG_POD" -c "$PG_CONTAINER" -- \
      psql -U postgres -c "ALTER USER admin_user WITH ENCRYPTED PASSWORD '$pass';"
    echo "  ✓ User admin_user password rotated"
  else
    kubectl -n "$NS" exec "$PG_POD" -c "$PG_CONTAINER" -- \
      psql -U postgres -c "CREATE USER admin_user WITH ENCRYPTED PASSWORD '$pass';"
    echo "  ✓ User admin_user created"
  fi
  kubectl -n "$NS" exec "$PG_POD" -c "$PG_CONTAINER" -- \
    psql -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE admin TO admin_user;" \
    >/dev/null
}

echo "→ admin-auth-secrets (NextAuth signer)"
ensure_secret_with_random admin-auth-secrets AUTH_SECRET \
  'openssl rand -base64 32'

echo "→ admin DB + user in shared iogrid-pg CNPG"
ensure_db_exists
PASS="admin_$(openssl rand -hex 16)"
ensure_user_with_password "$PASS"

echo "→ NextAuth Drizzle schema (DDL idempotent via IF NOT EXISTS)"
kubectl -n "$NS" exec -i "$PG_POD" -c "$PG_CONTAINER" -- \
  psql -U postgres -d admin <<'SQL'
CREATE TABLE IF NOT EXISTS "user" (
  id text PRIMARY KEY,
  name text,
  email text UNIQUE,
  "emailVerified" timestamp,
  image text
);
CREATE TABLE IF NOT EXISTS account (
  "userId" text NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  type text NOT NULL,
  provider text NOT NULL,
  "providerAccountId" text NOT NULL,
  refresh_token text,
  access_token text,
  expires_at integer,
  token_type text,
  scope text,
  id_token text,
  session_state text,
  PRIMARY KEY (provider, "providerAccountId")
);
CREATE TABLE IF NOT EXISTS session (
  "sessionToken" text PRIMARY KEY,
  "userId" text NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  expires timestamp NOT NULL
);
CREATE TABLE IF NOT EXISTS "verificationToken" (
  identifier text NOT NULL,
  token text NOT NULL,
  expires timestamp NOT NULL,
  PRIMARY KEY (identifier, token)
);
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO admin_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO admin_user;
SQL
echo "  ✓ NextAuth tables present"

echo "→ admin-db-secrets (DATABASE_URL for DrizzleAdapter)"
URL="postgres://admin_user:$PASS@iogrid-pg-rw.iogrid.svc.cluster.local:5432/admin?sslmode=require"
kubectl -n "$NS" create secret generic admin-db-secrets \
  --from-literal=DATABASE_URL="$URL" \
  --dry-run=client -o yaml | kubectl apply -f -
echo "  ✓ Secret/admin-db-secrets minted"

echo "→ Rolling admin Deployment"
kubectl -n "$NS" rollout restart deploy/admin
kubectl -n "$NS" rollout status deploy/admin --timeout=120s

# Give Traefik / Service endpoints time to converge — `rollout status`
# returns when the new ReplicaSet is desired+ready, but EndpointSlice
# propagation + Traefik upstream-refresh lags by ~10-20s. Without this
# the first probe hits the still-terminating pod → 502.
echo "→ Waiting 25s for endpoint convergence"
sleep 25

echo "→ Verifying /api/auth/session (with retry budget)"
attempts=0
max_attempts=6
while [ $attempts -lt $max_attempts ]; do
  CODE=$(curl -sS -o /dev/null -w "%{http_code}" \
           https://admin.iogrid.org/api/auth/session)
  attempts=$((attempts + 1))
  printf "  [%d/%d] HTTP %s\n" "$attempts" "$max_attempts" "$CODE"
  [ "$CODE" = "200" ] && break
  sleep 10
done
if [ "$CODE" != "200" ]; then
  echo "::error::session probe failed after $max_attempts attempts; inspect kubectl -n iogrid logs deploy/admin"
  exit 1
fi
# Final triple-check now that traffic is steady.
for i in 1 2 3; do
  CODE=$(curl -sS -o /dev/null -w "%{http_code}" \
           https://admin.iogrid.org/api/auth/session)
  [ "$CODE" = "200" ] || {
    echo "::error::steady-state probe [$i] failed (HTTP $CODE)"
    exit 1
  }
done
echo
echo "✓ admin NextAuth bootstrap complete. Sign-in flow ready."
