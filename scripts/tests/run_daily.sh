#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

MODE="${MODE:-quick}" # quick | lang_static | full | pre_release (full/pre_release: language-static first)
RUN_ID="${RUN_ID:-$(date -u +"%Y%m%dT%H%M%SZ")}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_MAX_UNSTABLE="${P2P_MAX_UNSTABLE:-1}"
P2P_MAX_BAD="${P2P_MAX_BAD:-1}"

token_is_placeholder() {
  local t="$1"
[[ "$t" == *"..."* || "$t" == *"PUT_FULL_TOKEN_HERE"* || "$t" == *"CHANGE_ME"* ]]
}

run_step() {
  local name="$1"
  shift
  echo "== $name =="
  "$@"
}

echo "== HackMe daily test runner =="
echo "RUN_ID=$RUN_ID MODE=$MODE BASE=$BASE COORD=$COORD"

if [[ "$MODE" == "full" || "$MODE" == "pre_release" ]]; then
  if [[ -z "$ADMIN_TOKEN" ]]; then
    echo "ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required for orders/security stages in MODE=$MODE" >&2
    exit 1
  fi
  if token_is_placeholder "$ADMIN_TOKEN"; then
    echo "ADMIN_TOKEN looks like placeholder; set real token value before MODE=$MODE run" >&2
    exit 1
  fi
fi

case "$MODE" in
  quick)
    run_step "transfers" env RUN_ID="$RUN_ID" BASE="$BASE" "$ROOT_DIR/scripts/tests/transfers_matrix.sh"
    run_step "security" env RUN_ID="$RUN_ID" BASE="$BASE" "$ROOT_DIR/scripts/tests/security_assertions.sh"
    ;;
  lang_static)
    run_step "language-static-pack" env STATIC_ONLY=1 RUN_ID="$RUN_ID" bash "$ROOT_DIR/scripts/tests/run_language_production_pack.sh"
    ;;
  full)
    run_step "language-static-pack" env STATIC_ONLY=1 RUN_ID="$RUN_ID" bash "$ROOT_DIR/scripts/tests/run_language_production_pack.sh"
    run_step "baseline" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/baseline_snapshot.sh"
    run_step "invariants" env BASE="$BASE" bash "$ROOT_DIR/scripts/check_invariants.sh"
    run_step "difficulty-health" env RUN_ID="$RUN_ID" BASE="$BASE" bash "$ROOT_DIR/scripts/tests/difficulty_health.sh"
    run_step "transfers" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/transfers_matrix.sh"
    run_step "orders" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/orders_matrix.sh"
    run_step "security" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/security_assertions.sh"
    run_step "adversarial-api" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}" "$ROOT_DIR/scripts/tests/adversarial_api_matrix.sh"
    run_step "p2p" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_MAX_UNSTABLE="$P2P_MAX_UNSTABLE" P2P_MAX_BAD="$P2P_MAX_BAD" "$ROOT_DIR/scripts/tests/p2p_smoke.sh"
    run_step "p2p-quality-matrix" env RUN_ID="$RUN_ID" "$ROOT_DIR/scripts/tests/p2p_quality_matrix.sh"
    run_step "gpu-hints-matrix" env RUN_ID="$RUN_ID" "$ROOT_DIR/scripts/tests/gpu_hints_matrix.sh"
    run_step "coordinator" env RUN_ID="$RUN_ID" BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/coordinator_matrix.sh"
    ;;
  pre_release)
    run_step "language-static-pack" env STATIC_ONLY=1 RUN_ID="$RUN_ID" bash "$ROOT_DIR/scripts/tests/run_language_production_pack.sh"
    run_step "private-stage-gate" env RUN_ID="$RUN_ID" BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/ops/private_stage_gate.sh"
    run_step "baseline" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/baseline_snapshot.sh"
    run_step "invariants" env BASE="$BASE" bash "$ROOT_DIR/scripts/check_invariants.sh"
    run_step "difficulty-health" env RUN_ID="$RUN_ID" BASE="$BASE" bash "$ROOT_DIR/scripts/tests/difficulty_health.sh"
    run_step "transfers" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/transfers_matrix.sh"
    run_step "orders" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/orders_matrix.sh"
    run_step "security" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/security_assertions.sh"
    run_step "adversarial-api" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}" "$ROOT_DIR/scripts/tests/adversarial_api_matrix.sh"
    run_step "p2p" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_MAX_UNSTABLE="$P2P_MAX_UNSTABLE" P2P_MAX_BAD="$P2P_MAX_BAD" "$ROOT_DIR/scripts/tests/p2p_smoke.sh"
    run_step "p2p-quality-matrix" env RUN_ID="$RUN_ID" "$ROOT_DIR/scripts/tests/p2p_quality_matrix.sh"
    run_step "gpu-hints-matrix" env RUN_ID="$RUN_ID" "$ROOT_DIR/scripts/tests/gpu_hints_matrix.sh"
    run_step "coordinator" env RUN_ID="$RUN_ID" BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" "$ROOT_DIR/scripts/tests/coordinator_matrix.sh"
    # Short soak defaults for practical pre-release gate; override via env if needed.
    run_step "soak" env RUN_ID="$RUN_ID" BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" DURATION_SEC="${DURATION_SEC:-1800}" INTERVAL_SEC="${INTERVAL_SEC:-120}" "$ROOT_DIR/scripts/tests/soak_capture.sh"
    ;;
  *)
    echo "Unknown MODE: $MODE" >&2
    echo "Allowed MODE values: quick, lang_static, full, pre_release" >&2
    exit 1
    ;;
esac

run_step "report" env RUN_ID="$RUN_ID" "$ROOT_DIR/scripts/tests/report_summary.sh"

echo "Done."
echo "Summary: $ROOT_DIR/reports/tests/$RUN_ID/summary_all.json"

