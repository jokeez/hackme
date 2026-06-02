#!/usr/bin/env bash
# Maximum confidence verdict: go tests + customer readiness + full validation suite.
# Usage: bash scripts/tests/max_verdict_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/max-verdict-$STAMP}"
mkdir -p "$OUT"
VERDICT="$OUT/VERDICT.md"
ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"
[[ -z "$ADMIN" && -f "$ROOT/.env.desktop" ]] && ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$ROOT/.env.desktop" | cut -d= -f2-)"
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN}"

log() { echo "[max-verdict] $*" | tee -a "$OUT/run.log"; }
run_step() {
  local id="$1"
  shift
  local elog="$OUT/steps/${id}.log"
  local eexit="$OUT/steps/${id}.exit"
  mkdir -p "$OUT/steps"
  log "=== $id ==="
  if "$@" >"$elog" 2>&1; then
    echo 0 >"$eexit"
    log "PASS $id"
    return 0
  fi
  ec=$?
  echo "$ec" >"$eexit"
  log "FAIL $id (exit $ec) — tail:"
  tail -20 "$elog" | tee -a "$OUT/run.log"
  return "$ec"
}

log "stamp=$STAMP out=$OUT"
if ! curl -fsS --max-time 20 "${BASE:-http://127.0.0.1:8080}/api/status?lite=1" >/dev/null 2>&1; then
  log "node down — starting via restart_linux_desktop_worker.sh"
  bash "$ROOT/scripts/ops/restart_linux_desktop_worker.sh" >>"$OUT/run.log" 2>&1 || true
  sleep 5
fi

FAIL=0
run_step go_test_short go test -short -count=1 ./... -timeout=300s || FAIL=$((FAIL + 1))
run_step customer_readiness bash "$ROOT/scripts/tests/customer_readiness_gate.sh" || FAIL=$((FAIL + 1))
run_step validation_suite_full env TIER=full SAFE_FOR_OVERNIGHT=0 STAMP="${STAMP}-vs" \
  bash "$ROOT/scripts/ops/run_validation_suite.sh" || FAIL=$((FAIL + 1))

VS_VERDICT="$ROOT/reports/validation-suite-${STAMP}-vs/VERDICT.md"
[[ -f "$VS_VERDICT" ]] && cp -f "$VS_VERDICT" "$OUT/validation_suite_VERDICT.md"

{
  echo "# Max confidence verdict — $STAMP"
  echo ""
  echo "Artifacts: \`$OUT/\`"
  echo ""
  echo "## Steps"
  for f in "$OUT"/steps/*.exit; do
    [[ -f "$f" ]] || continue
    id="$(basename "$f" .exit)"
    ec="$(cat "$f")"
    st=PASS
    [[ "$ec" != "0" ]] && st=FAIL
    echo "- **$id:** $st (exit $ec)"
  done
  echo ""
  if [[ -f "$OUT/validation_suite_VERDICT.md" ]]; then
    echo "## Validation suite (full tier)"
    echo ""
    sed -n '1,80p' "$OUT/validation_suite_VERDICT.md"
  fi
  echo ""
  if [[ "$FAIL" -eq 0 ]]; then
    echo "## Overall: **GO** — safe for prod customers (mining, audits, economics, gates)"
  else
    echo "## Overall: **WARN/NO-GO** — fix failed steps in \`$OUT/steps/*.log\`"
  fi
} >"$VERDICT"

log "→ $VERDICT (failed_steps=$FAIL)"
exit "$FAIL"
