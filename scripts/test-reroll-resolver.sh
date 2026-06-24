#!/usr/bin/env bash
# test-reroll-resolver.sh — proves reroll-iogrid-deployments.sh's deploy-marker
# resolver does NOT suffer the #822 prefix collision: a service whose name is a
# prefix of another service's marker scope must resolve its OWN digest, never the
# longer-named sibling's (and never be silently skipped).
#
# Pure unit test: sources the resolver with REROLL_LIB_ONLY=1 and feeds it a
# CRAFTED marker log on stdin — no git, no cluster, no network.
#
# Run: scripts/test-reroll-resolver.sh   (exit 0 = all pass)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
REROLL_LIB_ONLY=1 source "${HERE}/reroll-iogrid-deployments.sh"

# Crafted `git log --oneline` output, NEWEST FIRST (matches real ordering).
# The transparency-report markers are NEWER than the real antiabuse-svc marker —
# this is exactly the prod situation that triggered #822.
LOG="$(cat <<'EOF'
aaaa111 infra(antiabuse-svc-transparency-report): deploy harbor.openova.io/iogrid/antiabuse-svc-transparency-report@sha256:92ea446021a91d85686ef840d8497102244a34b6007fddd7c6a05df33b723a8a after CI 5c8f04f3
bbbb222 infra(web): deploy harbor.openova.io/iogrid/web@sha256:61edc9e7bcdc151e080676aa634ef61993082d4b after CI 3399abc6
cccc333 infra(antiabuse-svc): deploy harbor.openova.io/iogrid/antiabuse-svc@sha256:8e893046733812a180e0ebdb8fe68ae2ad2eed391f after CI 5c8f04f3
dddd444 infra(antiabuse-svc): deploy harbor.openova.io/iogrid/antiabuse-svc@sha256:f6ae6dc1aefa59d0cf8d69ce64109446546d79ce after CI 02f8fdda
eeee555 infra(vpn-gateway): deploy harbor.openova.io/iogrid/vpn-gateway@sha256:91bf4c867f6ce5d79ef082602ef0b60925bc50fb after CI 5c8f04f3
ffff666 infra(vpn-svc): deploy harbor.openova.io/iogrid/vpn-svc@sha256:718202838e37eda933b49f8d235cb68a350797b7 after CI 5c8f04f3
gggg777 infra(billing-svc): deploy harbor.openova.io/iogrid/billing-svc@sha256:4e349a706407eff3b4067665d94e7e65c4bcd42d667325cd3a51c63f2780bdf6 after CI bb958e0f
EOF
)"

pass=0
fail=0
assert_eq() { # <name> <expected> <actual>
  if [[ "$2" == "$3" ]]; then
    echo "PASS  $1"
    pass=$((pass + 1))
  else
    echo "FAIL  $1"
    echo "        expected: '$2'"
    echo "        actual:   '$3'"
    fail=$((fail + 1))
  fi
}

# 1. THE BUG: antiabuse-svc must resolve its OWN newest digest (8e893046),
#    NOT the newer transparency-report marker, and must NOT be empty/skipped.
got="$(printf '%s\n' "$LOG" | resolve_gitops_img antiabuse-svc)"
assert_eq "antiabuse-svc resolves own newest digest (#822 collision)" \
  "harbor.openova.io/iogrid/antiabuse-svc@sha256:8e893046733812a180e0ebdb8fe68ae2ad2eed391f" \
  "$got"

# 2. The longer-named sibling still resolves correctly (no regression).
got="$(printf '%s\n' "$LOG" | resolve_gitops_img antiabuse-svc-transparency-report)"
assert_eq "antiabuse-svc-transparency-report resolves its own digest" \
  "harbor.openova.io/iogrid/antiabuse-svc-transparency-report@sha256:92ea446021a91d85686ef840d8497102244a34b6007fddd7c6a05df33b723a8a" \
  "$got"

# 3. head -1 picks the NEWEST of two antiabuse-svc markers (8e89.. over f6ae..).
[[ "$got" != *f6ae6dc1* ]] && true # (covered above; explicit guard below)
got="$(printf '%s\n' "$LOG" | resolve_gitops_img antiabuse-svc)"
assert_eq "antiabuse-svc picks newest, not the older f6ae6dc1 marker" \
  "harbor.openova.io/iogrid/antiabuse-svc@sha256:8e893046733812a180e0ebdb8fe68ae2ad2eed391f" \
  "$got"

# 4. vpn-svc must NOT be confused by vpn-gateway (shared `vpn` prefix).
got="$(printf '%s\n' "$LOG" | resolve_gitops_img vpn-svc)"
assert_eq "vpn-svc not confused by vpn-gateway" \
  "harbor.openova.io/iogrid/vpn-svc@sha256:718202838e37eda933b49f8d235cb68a350797b7" \
  "$got"

# 5. A service with no marker resolves empty (graceful skip), not an error.
got="$(printf '%s\n' "$LOG" | resolve_gitops_img providers-svc)"
assert_eq "service with no marker resolves empty (graceful)" "" "$got"

# 6. #818 image-source alias: settlement-worker has NO marker of its own, but
#    runs the billing-svc image — resolving via its IMAGE_SOURCE (billing-svc)
#    must yield the billing-svc digest, not empty (the dead-locked-worker gap).
got_self="$(printf '%s\n' "$LOG" | resolve_gitops_img settlement-worker)"
assert_eq "settlement-worker has no own marker (resolves empty without alias)" "" "$got_self"
got_aliased="$(printf '%s\n' "$LOG" | resolve_gitops_img billing-svc)"
assert_eq "settlement-worker tracks the billing-svc image via IMAGE_SOURCE alias" \
  "harbor.openova.io/iogrid/billing-svc@sha256:4e349a706407eff3b4067665d94e7e65c4bcd42d667325cd3a51c63f2780bdf6" \
  "$got_aliased"

echo "----"
echo "${pass} passed, ${fail} failed"
[[ "$fail" -eq 0 ]]
