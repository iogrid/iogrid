#!/usr/bin/env bash
# docs-canonical-check.sh — pin the docs/ canonical structure from #337.
#
# Enforces the four binary success criteria from the original consolidation
# plan so a future fold/un-fold can't silently regress the layout:
#
#   1. Top-level docs/*.md count is ≤ 10 (target after the consolidation;
#      we started at 19).
#   2. No date-stamped files in flat docs/ (those belong in archive/ or
#      sessions/).
#   3. Every keeper that the consolidation plan promised exists on disk.
#   4. No flat docs/<deleted-orphan>.md re-introduced (TOKENOMICS, MARKET,
#      etc. — the 6 strategy orphans that PR #β folded).
#
# Returns 0 on pass, non-zero on the first violation. Print human-readable
# failure context so CI logs surface the issue at a glance.
#
# Run locally:  ./scripts/docs-canonical-check.sh
# In CI:        wire into the docs-lint job (separate from this script).

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

fail() {
    echo "❌ $*" >&2
    exit 1
}

# (1) Top-level count ≤ 10. Hard cap from the user-global §11 ceiling +
# the project-specific #337 fold plan.
count=$(find docs -maxdepth 1 -type f -name '*.md' | wc -l)
if [ "$count" -gt 10 ]; then
    echo "docs/ top-level *.md files (max 10):"
    find docs -maxdepth 1 -type f -name '*.md'
    fail "Top-level docs/*.md count is $count (limit: 10)."
fi

# (2) No date-stamped files in flat docs/. Sessions + archive carry the
# YYYY-MM-DD prefix; the flat directory is for canonical keepers only.
dated=$(find docs -maxdepth 1 -type f -name '*.md' | grep -E '20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]' || true)
if [ -n "$dated" ]; then
    fail "Date-stamped file(s) in flat docs/ (move to archive/ or sessions/):
$dated"
fi

# (3) Canonical keepers present. The fold plan in #337 promised these
# files exist; the README + cross-references depend on them.
canonical_keepers=(
    "docs/ARCHITECTURE.md"
    "docs/ROADMAP.md"
    "docs/BUSINESS-STRATEGY.md"
    "docs/SECURITY.md"
    "docs/RUNBOOKS.md"
)
for k in "${canonical_keepers[@]}"; do
    if [ ! -f "$k" ]; then
        fail "Canonical keeper missing on disk: $k (re-fold or restore)."
    fi
done

# (4) Folded orphans must NOT reappear in flat docs/. If someone restores
# one of these, the consolidation regressed.
#
# NOTE (2026-06-03): docs/TOKENOMICS.md and docs/MULTI_TENANT_MATRIX.md were
# REMOVED from this blocklist — they are legitimately-reinstated canonical docs
# (TOKENOMICS is referenced by contracts/tests/*, web staking.ts, mobile
# ping-pay.ts, SOLANA-ADDRESSES; MULTI_TENANT_MATRIX by the AASA route +
# BUSINESS-STRATEGY). They live in the flat docs/ dir by design now; the stale
# blocklist was failing CI on every docs commit. The genuinely-folded strategy
# orphans below stay enforced.
orphans=(
    "docs/INCENTIVES.md"
    "docs/LEGAL.md"
    "docs/MARKET.md"
    "docs/COMPETITORS.md"
    "docs/OFFRAMP_PROVIDERS.md"
    "docs/SOLANA.md"
    "docs/TECH.md"
    "docs/DNS_TLS.md"
    "docs/SECURITY-mTLS.md"
    "docs/RUNBOOK_STATUS.md"
    "docs/PHASE0-SETUP.md"
    "docs/PHASE0-UNBLOCK.md"
    "docs/PHASE0_FIRST_CUSTOMER.md"
    "docs/TRACKER.md"
)
for o in "${orphans[@]}"; do
    if [ -e "$o" ]; then
        fail "Folded orphan re-introduced: $o (delete it; fold target lives in BUSINESS-STRATEGY / ARCHITECTURE / RUNBOOKS / SECURITY / archive)."
    fi
done

echo "✅ docs/ canonical structure intact: $count top-level keepers, no date-stamped files in flat dir, all canonical keepers present, no folded-orphan regressions."
