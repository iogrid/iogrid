#!/usr/bin/env bash
# stripe-price-verify.sh — verify Stripe price IDs match the public commitment.
#
# Public commitment (web/src/app/vpn/page.tsx):
#   - Plus tier  → $2.99 / month → STRIPE_PRICE_STARTER
#   - Pro  tier  → $4.99 / month → STRIPE_PRICE_GROWTH
#
# Pulls the unit_amount from Stripe via REST, compares against the
# canonical pence amounts (299 / 499 — Stripe uses smallest currency
# unit; both tiers are USD so cents).
#
# Closes #444 when the four assertions pass.
#
# Pre-reqs:
#   - STRIPE_SECRET_KEY exported (use sk_live_… for production verify;
#     sk_test_… is fine for sanity-check against test mode).
#   - STRIPE_PRICE_STARTER + STRIPE_PRICE_GROWTH exported (the IDs
#     wired into the live billing-svc Deployment env). Get them from:
#     kubectl -n iogrid get secret billing-svc-secrets -o jsonpath='{.data}' | base64 -d
#
# Exit codes:
#   0 — both prices verified
#   1 — input missing
#   2 — API error
#   3 — unit_amount mismatch

set -euo pipefail

require() {
  local name=$1 hint=$2
  if [ -z "${!name:-}" ]; then
    echo "::error::missing env var $name — $hint"
    exit 1
  fi
}

require STRIPE_SECRET_KEY  "export STRIPE_SECRET_KEY=sk_live_… (or sk_test_…)"
require STRIPE_PRICE_STARTER "export STRIPE_PRICE_STARTER=price_… (Plus tier, \$2.99/mo)"
require STRIPE_PRICE_GROWTH  "export STRIPE_PRICE_GROWTH=price_…  (Pro tier,  \$4.99/mo)"

probe() {
  local price_id=$1 expected_cents=$2 expected_label=$3
  local resp
  if ! resp=$(curl -fsS "https://api.stripe.com/v1/prices/${price_id}" \
                -u "${STRIPE_SECRET_KEY}:" 2>&1); then
    echo "::error::Stripe API call failed for ${price_id}: ${resp}"
    exit 2
  fi

  local got_cents got_currency got_interval
  got_cents=$(printf '%s' "$resp" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("unit_amount", "ERR"))')
  got_currency=$(printf '%s' "$resp" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("currency", "ERR"))')
  got_interval=$(printf '%s' "$resp" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("recurring",{}).get("interval","ERR"))')

  printf '  %-12s id=%s amount=%s currency=%s interval=%s\n' \
    "$expected_label" "$price_id" "$got_cents" "$got_currency" "$got_interval"

  if [ "$got_cents" != "$expected_cents" ]; then
    echo "::error::${expected_label} unit_amount mismatch: expected $expected_cents, got $got_cents"
    exit 3
  fi
  if [ "$got_currency" != "usd" ]; then
    echo "::error::${expected_label} currency must be usd, got $got_currency"
    exit 3
  fi
  if [ "$got_interval" != "month" ]; then
    echo "::error::${expected_label} interval must be month, got $got_interval"
    exit 3
  fi
}

echo "→ Verifying STRIPE_PRICE_STARTER (Plus \$2.99/mo)"
probe "$STRIPE_PRICE_STARTER" 299 "Plus(\$2.99)"

echo "→ Verifying STRIPE_PRICE_GROWTH  (Pro  \$4.99/mo)"
probe "$STRIPE_PRICE_GROWTH"  499 "Pro(\$4.99)"

echo
echo "✓ Both tiers match the public commitment on https://iogrid.org/vpn"
echo "✓ Safe to flip public VPN traffic to live Stripe — #444 ready to close"
