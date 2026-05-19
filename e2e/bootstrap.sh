#!/usr/bin/env bash
# bootstrap.sh — install lightweight infra into the kind cluster.
#
# We deliberately AVOID the heavy operators that prod uses (CNPG, Cilium,
# cert-manager) because they each take 30-60s extra + add CRDs that we
# don't exercise in smoke flows. Instead:
#
#   - Plain Postgres 16 StatefulSet with per-service databases (no CNPG CRDs)
#   - Plain NATS 2.10 single-replica (JetStream enabled in file mode)
#   - MailHog 1.0.1 for SMTP intake (replaces Stalwart in dev)
#   - A self-signed test CA + a single wildcard *.svc cert mounted as a
#     Secret (replaces cert-manager Issuer + Certificate resources)
#
# Once these pods are Ready the rest of the harness can deploy
# scaffold-tagged iogrid service images and exercise the JSON / Connect-RPC
# endpoints.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=${NAMESPACE:-iogrid}

log() { printf '%s [bootstrap] %s\n' "$(date -u +%FT%TZ)" "$*"; }

# --- Namespace + default-deny opt-out --------------------------------------
log "creating namespace $NS"
kubectl create namespace "$NS" --dry-run=client -o yaml \
  | kubectl apply -f -
kubectl label namespace "$NS" \
  app.kubernetes.io/part-of=iogrid \
  pod-security.kubernetes.io/enforce=baseline \
  --overwrite

# --- Test secrets -----------------------------------------------------------
# These are deliberately NOT real credentials. Every value is a static
# string scoped to the kind cluster lifetime; nothing leaks outside the
# cluster boundary.
log "applying test-stub secrets"
kubectl -n "$NS" apply -f "$HERE/manifests/secrets/test-secrets.yaml"

# --- Self-signed test cert (replaces cert-manager Issuer) ------------------
log "generating self-signed CA + wildcard cert"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$TMP/ca.key" -out "$TMP/ca.crt" \
  -subj "/CN=iogrid-e2e-ca" -days 1 2>/dev/null
# Wildcard for *.iogrid.svc.cluster.local (used by service-to-service)
openssl req -newkey rsa:2048 -nodes -keyout "$TMP/svc.key" -out "$TMP/svc.csr" \
  -subj "/CN=*.iogrid.svc.cluster.local" 2>/dev/null
cat >"$TMP/svc.ext" <<EOF
subjectAltName=DNS:*.iogrid.svc.cluster.local,DNS:*.iogrid,DNS:localhost,IP:127.0.0.1
EOF
openssl x509 -req -in "$TMP/svc.csr" -CA "$TMP/ca.crt" -CAkey "$TMP/ca.key" \
  -CAcreateserial -out "$TMP/svc.crt" -days 1 -extfile "$TMP/svc.ext" 2>/dev/null
kubectl -n "$NS" create secret tls iogrid-e2e-tls \
  --cert "$TMP/svc.crt" --key "$TMP/svc.key" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" create secret generic iogrid-e2e-ca \
  --from-file=ca.crt="$TMP/ca.crt" \
  --dry-run=client -o yaml | kubectl apply -f -

# --- Plain Postgres (replaces CNPG) ----------------------------------------
log "deploying Postgres 16"
kubectl -n "$NS" apply -f "$HERE/manifests/postgres/postgres.yaml"

# --- Plain NATS JetStream (single replica) ---------------------------------
log "deploying NATS"
kubectl -n "$NS" apply -f "$HERE/manifests/nats/nats.yaml"

# --- MailHog for magic-link delivery ---------------------------------------
log "deploying MailHog"
kubectl -n "$NS" apply -f "$HERE/manifests/mailhog/mailhog.yaml"

# --- Wait for everything to be ready ---------------------------------------
log "waiting for postgres rollout"
kubectl -n "$NS" rollout status statefulset/postgres --timeout=180s
log "waiting for nats rollout"
kubectl -n "$NS" rollout status statefulset/nats --timeout=180s
log "waiting for mailhog rollout"
kubectl -n "$NS" rollout status deployment/mailhog --timeout=120s

# --- Prime per-service databases via psql ----------------------------------
log "creating per-service databases"
PG_POD=$(kubectl -n "$NS" get pod -l app=postgres -o jsonpath='{.items[0].metadata.name}')
for db in identity providers workloads antiabuse billing telemetry; do
  kubectl -n "$NS" exec "$PG_POD" -- \
    psql -U postgres -tAc "CREATE DATABASE $db;" >/dev/null 2>&1 || true
done

log "bootstrap complete"
