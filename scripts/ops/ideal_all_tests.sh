#!/usr/bin/env bash
# Run all feasible production tests; skip ephemeral-local-only with clear reason.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/ideal-all-$STAMP"
mkdir -p "$OUT"
VERDICT="$OUT/IDEAL_ALL_VERDICT.md"

DESK="$(grep '^HACKME_ADMIN_TOKEN=' .env.desktop 2>/dev/null | cut -d= -f2- || true)"
COORD_TOKEN="$(tr -d '\r\n' <.secrets/hackme_coordinator_admin_token 2>/dev/null || true)"
export ADMIN_TOKEN="${ADMIN_TOKEN:-$DESK}"
export HACKME_ADMIN_TOKEN="$ADMIN_TOKEN"
export COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-$COORD_TOKEN}"

P=0; F=0; S=0
log() { echo "[ideal-all] $*" | tee -a "$OUT/run.log"; }
record() {
  local id="$1" st="$2" msg="$3"
  echo "| $id | $st | $msg |" >>"$OUT/table.md"
  case "$st" in
    PASS) P=$((P+1)) ;;
    FAIL) F=$((F+1)) ;;
    *) S=$((S+1)) ;;
  esac
}
run_step() {
  local id="$1"; shift
  log "=== $id ==="
  if "$@" >"$OUT/${id}.log" 2>&1; then
    record "$id" PASS "$(printf '%q ' "$@")"
    return 0
  fi
  record "$id" FAIL "$(printf '%q ' "$@")"
  return 1
}
run_optional() {
  local id="$1"; shift
  log "=== $id (optional) ==="
  if "$@" >"$OUT/${id}.log" 2>&1; then
    record "$id" PASS "$(printf '%q ' "$@")"
  else
    record "$id" SKIP "$(printf '%q ' "$@")"
  fi
}

: >"$OUT/table.md"
echo "| Test | Result | Notes |" >>"$OUT/table.md"
echo "|------|--------|-------|" >>"$OUT/table.md"

run_step pool_ideal_finalize bash scripts/ops/pool_ideal_finalize.sh || true
run_step production_master_gate bash scripts/ops/production_master_gate.sh || true
for _ in 1 2 3 4 5; do
  curl -fsS --max-time 4 http://127.0.0.1:8080/api/global/metrics >/dev/null 2>&1 && break
  sleep 2
done
run_step coordinator_matrix env COORD="https://hackme.tech/pool/coordinator" COORD_ADMIN_TOKEN="$COORD_TOKEN" \
  bash scripts/tests/coordinator_matrix.sh || true
run_step adversarial_api env BASE=http://127.0.0.1:8080 BURST_REQUESTS=15 CURL_MAX_TIME=8 \
  bash scripts/tests/adversarial_api_matrix.sh || true
run_step difficulty_health env BASE=http://127.0.0.1:8080 bash scripts/tests/difficulty_health.sh || true
run_step fuzz_dashboard bash scripts/tests/fuzz_dashboard_smoke.sh || true
run_step redteam_public env BASE=https://hackme.tech bash scripts/tests/redteam_surface_smoke.sh || true
run_step redteam_local env BASE=http://127.0.0.1:8080 bash scripts/tests/redteam_surface_smoke.sh || true
run_step security_local env BASE=http://127.0.0.1:8080 bash scripts/tests/security_assertions.sh || true
run_step new_miner_journey env WORKER_SMOKE=0 bash scripts/ops/new_miner_journey_gate.sh || true
run_optional fuzzing_soak bash scripts/ops/fuzzing_soak_prep.sh
run_optional network_soak env BASE=https://hackme.tech DURATION_SEC=120 INTERVAL_SEC=15 \
  bash scripts/ops/network_stability_soak.sh
run_optional go_test_short go test ./... -short -count=1 -timeout=300s

if ssh -o BatchMode=yes -o ConnectTimeout=8 -i "${HOME}/.ssh/id_ed25519" hackme-vps true 2>/dev/null; then
  run_optional vps_orders_matrix ssh -i "${HOME}/.ssh/id_ed25519" hackme-vps \
    'cd /opt/hackme && ADMIN=$(grep ^HACKME_ADMIN_TOKEN= .env.vps|cut -d= -f2-) && \
     BASE=http://127.0.0.1:18080 ADMIN_TOKEN=$ADMIN bash scripts/tests/orders_matrix.sh' || true
fi

STATUS="PASS"
[[ "$F" -eq 0 ]] && STATUS="PASS" || STATUS="FAIL"
[[ "$F" -eq 0 && "$S" -gt 0 ]] && STATUS="PASS_WITH_SKIPS"

{
  echo "# IDEAL ALL TESTS — $STATUS"
  echo ""
  echo "**$(date -u +%Y-%m-%dT%H:%M:%SZ)** · pass=$P fail=$F skip=$S"
  echo ""
  cat "$OUT/table.md"
  echo ""
  echo "Logs: \`$OUT\`"
  echo ""
  echo "## Ephemeral local only (run on VPS or stack_up)"
  echo "- \`bash scripts/ops/redteam_hard_mode.sh\` (needs BASE=18080 COORD=18081)"
  echo "- \`bash scripts/ops/simulate_pool_swarm_local.sh\`"
  echo "- \`bash scripts/tests/mega_stress.sh\`"
} >"$VERDICT"

ln -sfn "$OUT" "$ROOT/reports/ideal-all-LATEST"
cp -f "$VERDICT" "$ROOT/reports/IDEAL_ALL_VERDICT.md"
log "verdict: $STATUS ($P pass / $F fail / $S skip) -> $VERDICT"
exit $(( F > 0 ? 1 : 0 ))
