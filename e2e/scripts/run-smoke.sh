#!/usr/bin/env bash
# run-smoke.sh — execute every smoke/*.sh, capture stdout/stderr, emit
# a single JUnit XML for upload.
#
# Usage: run-smoke.sh <output-dir>

set -euo pipefail
OUT_DIR=${1:-./out/smoke}
SMOKE_DIR="$(cd "$(dirname "$0")"/../smoke && pwd)"
mkdir -p "$OUT_DIR"

# All flows ALWAYS run — failures don't short-circuit so the JUnit
# report captures the full matrix.
TOTAL=0
FAIL=0
declare -a CASES

start_ts=$(date +%s)

for f in "$SMOKE_DIR"/*.sh; do
  name="$(basename "$f" .sh)"
  TOTAL=$((TOTAL + 1))
  log="$OUT_DIR/$name.log"
  printf '\n==> smoke: %s\n' "$name"
  t0=$(date +%s)
  set +e
  bash "$f" >"$log" 2>&1
  rc=$?
  set -e
  t1=$(date +%s)
  dur=$((t1 - t0))
  if [ $rc -eq 0 ]; then
    echo "    PASS ($dur s)"
    CASES+=("$name|$dur|PASS|")
  else
    echo "    FAIL ($dur s) — see $log"
    FAIL=$((FAIL + 1))
    last_err="$(tail -1 "$log" | head -c 240)"
    CASES+=("$name|$dur|FAIL|$last_err")
  fi
done

end_ts=$(date +%s)
total_dur=$((end_ts - start_ts))

# --- emit JUnit XML --------------------------------------------------------
junit="$OUT_DIR/junit.xml"
{
  echo '<?xml version="1.0" encoding="UTF-8"?>'
  printf '<testsuites name="iogrid-e2e" tests="%d" failures="%d" time="%d">\n' \
    "$TOTAL" "$FAIL" "$total_dur"
  printf '  <testsuite name="smoke" tests="%d" failures="%d" time="%d">\n' \
    "$TOTAL" "$FAIL" "$total_dur"
  for c in "${CASES[@]}"; do
    IFS='|' read -r n d r e <<<"$c"
    printf '    <testcase name="%s" classname="smoke" time="%s">\n' "$n" "$d"
    if [ "$r" = "FAIL" ]; then
      e_escaped="${e//&/&amp;}"
      e_escaped="${e_escaped//</&lt;}"
      e_escaped="${e_escaped//>/&gt;}"
      printf '      <failure message="%s"/>\n' "$e_escaped"
    fi
    echo '    </testcase>'
  done
  echo '  </testsuite>'
  echo '</testsuites>'
} >"$junit"

# --- summary matrix --------------------------------------------------------
echo
echo "===== iogrid e2e smoke matrix ====="
printf '%-30s %-7s %-6s\n' "TEST" "RESULT" "SECS"
printf '%-30s %-7s %-6s\n' "----" "------" "----"
for c in "${CASES[@]}"; do
  IFS='|' read -r n d r e <<<"$c"
  printf '%-30s %-7s %-6s\n' "$n" "$r" "$d"
done
echo
echo "Total: $TOTAL  Failed: $FAIL  Duration: ${total_dur}s"
echo "JUnit: $junit"
echo "Logs:  $OUT_DIR/<name>.log"
echo "==================================="

# Always exit 0 — e2e is informational; CI sets continue-on-error.
exit 0
