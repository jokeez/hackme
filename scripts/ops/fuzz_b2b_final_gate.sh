#!/usr/bin/env bash
# B2B fuzz final gate — aggregates security + product readiness (local-first; prod optional).
#
# Usage:
#   bash scripts/ops/fuzz_b2b_final_gate.sh
#   ADMIN_TOKEN=… BASE=http://127.0.0.1:8080 bash scripts/ops/fuzz_b2b_final_gate.sh
#
# Writes: reports/fuzz-b2b-final-<stamp>/VERDICT.md
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/fuzz-b2b-final-$STAMP}"
mkdir -p "$OUT"
LOG="$OUT/run.log"
VERDICT="$OUT/VERDICT.md"
ADMIN="$(tr -d '\r\n' <"${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}" 2>/dev/null || true)"
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN}"
export ADMIN_TOKEN="${ADMIN_TOKEN:-$ADMIN}"
BASE_LOCAL="${BASE:-${BASE_LOCAL:-http://127.0.0.1:8080}}"
BASE_PROD="${BASE_PROD:-https://hackme.tech}"

pass_n=0
fail_n=0
skip_n=0

record() {
  local id="$1" status="$2" detail="$3"
  echo "[$status] $id — $detail" | tee -a "$LOG"
  printf '%s\t%s\t%s\n' "$id" "$status" "$detail" >>"$OUT/steps.tsv"
  case "$status" in
    pass) pass_n=$((pass_n + 1)) ;;
    fail) fail_n=$((fail_n + 1)) ;;
    skip) skip_n=$((skip_n + 1)) ;;
  esac
}

run_step() {
  local id="$1"
  shift
  echo "=== $id ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    record "$id" "pass" "$*"
    return 0
  fi
  record "$id" "fail" "$* (see $LOG)"
  return 1
}

run_optional() {
  local id="$1"
  shift
  echo "=== $id (optional) ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    record "$id" "pass" "$*"
  else
    record "$id" "skip" "$*"
  fi
}

{
  echo "# Fuzz B2B Final Gate"
  echo ""
  echo "- stamp: \`$STAMP\`"
  echo "- out: \`$OUT\`"
  echo ""
} >"$VERDICT"

echo "[fuzz-b2b-final] starting — out=$OUT" | tee -a "$LOG"

# Optional ephemeral node when local BASE is down (isolated data dir; stable binary).
EPHEMERAL_STARTED=0
if ! curl -fsS --max-time 5 "${BASE_LOCAL}/api/status?lite=1" >/dev/null 2>&1; then
  if [[ "${AUTO_EPHEMERAL_NODE:-1}" == "1" ]]; then
    # shellcheck source=scripts/tests/ephemeral_hackme_node.sh
    source "$ROOT/scripts/tests/ephemeral_hackme_node.sh"
    EPHEMERAL_BIND="${EPHEMERAL_BIND:-${BASE_LOCAL#http://}}"
    EPHEMERAL_PORT="${EPHEMERAL_PORT:-${EPHEMERAL_BIND##*:}}"
    EPHEMERAL_BASE="$BASE_LOCAL"
    if ephemeral_hackme_node_start >>"$LOG" 2>&1; then
      EPHEMERAL_STARTED=1
      ADMIN="$EPHEMERAL_ADMIN_TOKEN"
      export ADMIN_TOKEN="$ADMIN" HACKME_ADMIN_TOKEN="$ADMIN"
      record "ephemeral-node-start" "pass" "$BASE_LOCAL pid=$EPHEMERAL_NODE_PID"
      trap '[[ "$EPHEMERAL_STARTED" == 1 ]] && ephemeral_hackme_node_stop' EXIT
    else
      record "ephemeral-node-start" "skip" "could not start ephemeral node at $BASE_LOCAL"
    fi
  fi
fi

# --- Always (no live node) ---
run_step site-b2b-content bash "$ROOT/scripts/tests/site_b2b_content_gate.sh"
run_step go-fuzzingcli-packages go test ./internal/fuzzingcli/ ./cmd/fuzzingclient/ -count=1 -timeout=90s
run_step go-fuzzengine-depth go test ./internal/fuzzengine/ -count=1 -timeout=120s
run_step critical-security-pack bash "$ROOT/scripts/tests/critical_security_pack.sh"
run_step pool-fuzz-redteam bash "$ROOT/scripts/ops/pool_fuzz_escrow_redteam_gate.sh"
run_step coordinator-wasm-gate go test ./cmd/coordinator/ -run TestWasmGateServerRejectsFabricatedPass -count=1 -timeout=60s

# Build CLI for smokes
if bash "$ROOT/scripts/ops/build_fuzzing_client.sh" >>"$LOG" 2>&1; then
  record "build-fuzzing-cli" "pass" "build_fuzzing_client.sh"
else
  record "build-fuzzing-cli" "fail" "build_fuzzing_client.sh"
fi

NODE_UP=0
if curl -fsS --max-time 5 "${BASE_LOCAL}/api/status?lite=1" >/dev/null 2>&1; then
  NODE_UP=1
  record "local-node-up" "pass" "$BASE_LOCAL"
else
  record "local-node-up" "skip" "node down at $BASE_LOCAL"
fi

if [[ "$NODE_UP" -eq 1 && -n "$ADMIN" ]]; then
  run_step fuzz-runtime-gate env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN" \
    bash "$ROOT/scripts/tests/fuzz_runtime_gate.sh"
  run_step fuzz-marketplace-lifecycle env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN" \
    bash "$ROOT/scripts/tests/fuzz_marketplace_lifecycle_gate.sh"
  run_step fuzzing-cli-smoke env BASE="$BASE_LOCAL" \
    bash "$ROOT/scripts/tests/fuzzing_cli_smoke.sh"
  run_step fuzz-b2b-wizard-smoke env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN" \
    bash "$ROOT/scripts/tests/fuzz_b2b_wizard_smoke.sh"
  run_step fuzzing-portal-api-local env BASE="$BASE_LOCAL" \
    bash "$ROOT/scripts/tests/fuzzing_portal_api_smoke.sh"
  if [[ "${RUN_FULL_FUZZ_RELEASE_GATE:-0}" == "1" ]]; then
    run_step fuzz-release-gate env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN" \
      bash "$ROOT/scripts/ops/fuzz_release_gate.sh"
  else
    record "fuzz-release-gate" "skip" "set RUN_FULL_FUZZ_RELEASE_GATE=1 for 13-step release gate"
  fi
else
  record "local-live-gates" "skip" "need local node + admin token"
fi

run_optional fuzz-public-hardening env BASE="$BASE_PROD" \
  bash "$ROOT/scripts/tests/fuzzing_public_hardening_smoke.sh"

run_optional fuzzing-developer-portal-prod env BASE="$BASE_PROD" \
  bash "$ROOT/scripts/tests/fuzzing_developer_portal_smoke.sh"

run_optional pool-fuzz-sync env BASE="$BASE_LOCAL" \
  bash "$ROOT/scripts/tests/pool_fuzz_sync_gate.sh"

run_optional hmc-customer-verdict bash "$ROOT/scripts/tests/hmc_customer_verdict_gate.sh"

if [[ "${RUN_MAX_RESILIENCE:-0}" == "1" ]]; then
  run_step maximum-resilience env STRESS_QUICK=1 RUN_ID="b2b_final_${STAMP}" \
    bash "$ROOT/scripts/tests/maximum_resilience_gate.sh"
else
  record "maximum-resilience" "skip" "set RUN_MAX_RESILIENCE=1 for full stress"
fi

OVERALL="GREEN"
if [[ "$fail_n" -gt 0 ]]; then
  OVERALL="RED"
elif [[ "$skip_n" -gt 0 ]]; then
  OVERALL="YELLOW"
fi

{
  echo "## Summary"
  echo ""
  echo "| Metric | Count |"
  echo "|--------|-------|"
  echo "| pass | $pass_n |"
  echo "| fail | $fail_n |"
  echo "| skip | $skip_n |"
  echo ""
  echo "**Verdict: $OVERALL**"
  echo ""
  echo "## Steps"
  echo ""
  echo '```'
  column -t -s $'\t' "$OUT/steps.tsv" 2>/dev/null || cat "$OUT/steps.tsv"
  echo '```'
  echo ""
  echo "## B2B packages"
  echo ""
  echo "| Package | depth_tier | HMC | runs | pool |"
  echo "|---------|------------|-----|------|------|"
  echo "| scan | wasm_only | 1 | 64 | local |"
  echo "| audit | wasm_native | 5 | 256 | yes |"
  echo "| deep | bytes_corpus | 10 | 1000 | yes |"
  echo ""
  echo "## Customer flow"
  echo ""
  echo '```bash'
  echo 'hackme-fuzzing wizard --wasm ./guard.wasm --package audit --title "My guard"'
  echo '# → campaign_id, customer_report_token, report_url, gate_url'
  echo '```'
} >>"$VERDICT"

echo ""
echo "[fuzz-b2b-final] VERDICT=$OVERALL pass=$pass_n fail=$fail_n skip=$skip_n"
echo "[fuzz-b2b-final] report: $VERDICT"

if [[ "$OVERALL" == "RED" ]]; then
  exit 1
fi
exit 0
