#!/usr/bin/env bash
# Language + WASM surface pack for production readiness (existing languages first).
#
# Phase A (no running node): manifest lint + WASM ABI check on repo artifacts.
# Phase B (running node + ADMIN_TOKEN): from_code matrix, multilang orders audit,
#   break attempts, chaos security, red-team HTTP smoke.
#
# Usage (repo root):
#   # Static only:
#   STATIC_ONLY=1 bash scripts/tests/run_language_production_pack.sh
#
#   # Full (needs genesis node with compilers on PATH, same as fuzz_release_gate language stages):
#   ADMIN_TOKEN=... BASE=http://127.0.0.1:8080 bash scripts/tests/run_language_production_pack.sh
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd go
STATIC_ONLY="${STATIC_ONLY:-0}"
BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
RID="${RUN_ID:-lang_prod_$(run_id)}"
export RUN_ID="$RID"
ensure_reports_dir "$ROOT_DIR/reports/tests/$RID"

echo "== HackMe language production pack run_id=$RUN_ID static_only=$STATIC_ONLY =="
echo "Supported from_code langs (see docs/TASK_LANGUAGES.md): rust, c, cpp/c++, gcc, zig,"
echo "  assemblyscript/as, tinygo, go→tinygo, wat (+ negative cases)."
echo ""

echo "== [1/6] task_manifest_lint (all manifests under tasks/manifests/) =="
shopt -s nullglob
manifests=(tasks/manifests/*.json)
shopt -u nullglob
if ((${#manifests[@]} == 0)); then
  warn "no tasks/manifests/*.json — skipping manifest lint"
else
  go run ./tools/task_manifest_lint "${manifests[@]}"
fi

echo "== [2/6] task_abi_check (all tasks/**/*.wasm) =="
mapfile -t wasm_files < <(find tasks -name '*.wasm' -type f 2>/dev/null | sort -u || true)
if ((${#wasm_files[@]} == 0)); then
  warn "no wasm under tasks/ — skipping ABI check"
else
  go run ./tools/task_abi_check "${wasm_files[@]}"
fi

if [[ "$STATIC_ONLY" == "1" ]]; then
  echo "STATIC_ONLY=1 — phases 3–6 skipped (no live node)."
  ensure_reports_dir "$ROOT_DIR/reports/tests/$RID/language_static"
  ms="$([[ ${#manifests[@]} -gt 0 ]] && echo 1 || echo 0)"
  ws="$([[ ${#wasm_files[@]} -gt 0 ]] && echo 1 || echo 0)"
  st_total=$((ms + ws))
  [[ "$st_total" -gt 0 ]] || st_total=1
  jq -nc \
    --arg run_id "$RID" \
    --arg captured_at "$(ts_utc)" \
    --argjson total "$st_total" \
    '{run_id:$run_id,captured_at:$captured_at,status:"PASS",total:$total,fails:0,note:"manifest_lint+wasm_abi"}' \
    >"$ROOT_DIR/reports/tests/$RID/language_static/summary.json"
  pass "language production pack (static) PASS — see reports/tests/$RID/"
  exit 0
fi

if [[ -z "$ADMIN_TOKEN" ]]; then
  fail "ADMIN_TOKEN or HACKME_ADMIN_TOKEN required for live phases (or STATIC_ONLY=1)"
fi

require_cmd curl
require_cmd jq

echo "== [3/6] language_from_code_matrix =="
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" RUN_ID="$RID" bash "$ROOT_DIR/scripts/tests/language_from_code_matrix.sh"

echo "== [4/6] orders_multilang_audit =="
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" RUN_ID="$RID" bash "$ROOT_DIR/scripts/tests/orders_multilang_audit.sh"

echo "== [5/6] language_break_attempts + language_chaos_security =="
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" RUN_ID="$RID" bash "$ROOT_DIR/scripts/tests/language_break_attempts.sh"
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" RUN_ID="$RID" bash "$ROOT_DIR/scripts/tests/language_chaos_security.sh"

echo "== [6/6] redteam_surface_smoke (non-destructive) =="
BASE="$BASE" RUN_ID="$RID" bash "$ROOT_DIR/scripts/tests/redteam_surface_smoke.sh"

pass "language production pack FULL PASS — reports/tests/$RID/"
