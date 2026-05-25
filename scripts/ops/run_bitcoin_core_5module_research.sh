#!/usr/bin/env bash
# Five Bitcoin-Core-inspired WASM modules: build, order, property-fuzz, report.
#
#   bash scripts/ops/run_bitcoin_core_5module_research.sh
#   BASE=https://hackme.tech ADMIN_FILE=$PWD/.secrets/hackme_admin_token bash scripts/ops/run_bitcoin_core_5module_research.sh
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
RUNS="${RUNS:-80}"
OUT="${OUT:-$ROOT/reports/bitcoin-core-5module}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"

require_cmd curl
require_cmd jq
require_cmd python3
require_cmd go

ADMIN="$(head -n1 "$ADMIN_FILE" | tr -d '\r\n')"
if [[ -z "$ADMIN" ]]; then
  echo "[bc5] missing admin token in $ADMIN_FILE" >&2
  exit 1
fi

mkdir -p "$OUT"
HDR=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

# module_id|guard|bc_ref
MODULES=(
  "1|script_push_bounds_guard|script.h MAX_SCRIPT_ELEMENT_SIZE + script.cpp GetScriptOp"
  "2|bounds_guard|script.cpp HasValidOps push size"
  "3|overflow_guard|consensus/tx_check.cpp MoneyRange-style"
  "4|state_transition_guard|validation.cpp state accept (simplified)"
  "5|cpp_script_push_bounds_guard|script.cpp GetScriptOp (C++ guard)"
)

if [[ "${SKIP_BUILD:-0}" == "1" ]]; then
  echo "[bc5] SKIP_BUILD=1 (use prebuilt tasks/artifacts/security)"
else
  echo "[bc5] build security pack"
  bash "$ROOT/scripts/build_security_task_pack.sh" >/dev/null
fi

echo "[bc5] node status"
curl -fsS "$BASE/api/status" "${HDR[@]}" | jq -c '{has_genesis,mining,tip_height,version}' | tee "$OUT/status_${TS}.json"

WALLET_BEFORE="$(curl -fsS "$BASE/api/wallet" "${HDR[@]}" | jq -c '{address,balance_hmc}')"
echo "[bc5] wallet before: $WALLET_BEFORE"
echo "$WALLET_BEFORE" >"$OUT/wallet_before_${TS}.json"

: >"$OUT/results_${TS}.jsonl"

run_module() {
  local num="$1" guard="$2" bc_ref="$3"
  local order_id="order-bc5-m${num}-${guard}-${TS}"
  local cid="campaign-bc5-m${num}-${TS}"
  local wasm="$ROOT/tasks/artifacts/security/rust_${guard}.wasm"
  local manifest="$ROOT/tasks/manifests/security/order-rust-${guard}-001.json"
  if [[ "$guard" == cpp_script_push_bounds_guard ]]; then
    wasm="$ROOT/tasks/artifacts/security/cpp_${guard}.wasm"
    manifest="$ROOT/tasks/manifests/security/order-cpp-${guard}-001.json"
  fi

  echo ""
  echo "========== module $num: $guard =========="
  echo "  Core: $bc_ref"

  go run ./tools/task_abi_check "$wasm" >/dev/null
  go run ./tools/task_manifest_lint "$manifest" >/dev/null

  local body
  body="$(python3 - <<PY
import json, pathlib
m = json.loads(pathlib.Path("$manifest").read_text())
m["id"] = "$order_id"
m["payer_ref"] = "research:bitcoin-core-5module:m$num"
m["reward_hmc"] = 0.01
m["target_solves"] = 1
m["difficulty_score"] = 10
w = pathlib.Path("$wasm").read_bytes()
m["wasm_check_hex"] = w.hex()
m.pop("wasm_artifact_path", None)
print(json.dumps(m))
PY
)"

  local post_code
  post_code="$(curl -sS -o "$OUT/order_${order_id}.json" -w '%{http_code}' -X POST "$BASE/api/tasks" \
    "${HDR[@]}" --data-binary "$body")"
  echo "  order POST HTTP $post_code"
  jq -c '{ok,id,prepaid_hmc,total_debit_hmc,error}' "$OUT/order_${order_id}.json" || true
  if [[ "$post_code" != "200" ]]; then
    jq -nc --arg n "$num" --arg g "$guard" --arg e "order_post_$post_code" \
      '{module:$n,guard:$g,verdict:"error",detail:$e}' >>"$OUT/results_${TS}.jsonl"
    return
  fi

  curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${HDR[@]}" -d "$(jq -nc \
    --arg id "$cid" --arg title "bc5-m${num}-${guard}" --arg task "$order_id" --argjson runs "$RUNS" \
    '{id:$id,campaign_type:"property",status:"planned",title:$title,budget_runs:$runs,owner_ref:"research:bitcoin-core",task_id:$task,config:{auto_runner:"1",worker_batch:16}}')" \
    | tee "$OUT/campaign_create_${cid}.json" | jq -c '{ok,id:.campaign.id,type:.campaign.campaign_type}'

  echo "  waiting fuzz autorunner (max 120s)..."
  local verdict="" conf="" crit="" high="" i
  for i in $(seq 1 24); do
    sleep 5
    local rep
    rep="$(curl -fsS "$BASE/api/fuzz/campaigns/${cid}/report?format=json&limit=30" "${HDR[@]}" 2>/dev/null || echo '{}')"
    local st
    st="$(echo "$rep" | jq -r '.campaign.status // .status // empty')"
    verdict="$(echo "$rep" | jq -r '.security_summary.verdict // .verdict // empty')"
    conf="$(echo "$rep" | jq -r '.security_summary.confidence // empty')"
    crit="$(echo "$rep" | jq -r '.security_summary.critical_count // 0')"
    high="$(echo "$rep" | jq -r '.security_summary.high_count // 0')"
    echo "    t=$i status=$st verdict=$verdict crit=$crit high=$high"
    if [[ "$st" == "completed" || "$st" == "failed" ]]; then
      echo "$rep" >"$OUT/report_${cid}.json"
      break
    fi
  done

  local order_st
  order_st="$(curl -fsS "$BASE/api/tasks" "${HDR[@]}" | jq -r --arg id "$order_id" '.tasks[]|select(.id==$id)|.status' || echo '?')"

  jq -nc \
    --arg n "$num" --arg g "$guard" --arg bc "$bc_ref" \
    --arg oid "$order_id" --arg cid "$cid" --arg ov "$order_st" \
    --arg v "${verdict:-timeout}" --argjson c "${conf:-0}" --argjson cr "${crit:-0}" --argjson hi "${high:-0}" \
    '{module:$n,guard:$g,bitcoin_core:$bc,order_id:$oid,campaign_id:$cid,order_status:$ov,verdict:$v,confidence:$c,critical:$cr,high:$hi}' \
    >>"$OUT/results_${TS}.jsonl"
}

while IFS='|' read -r num guard bc_ref; do
  run_module "$num" "$guard" "$bc_ref"
done < <(printf '%s\n' "${MODULES[@]}")

WALLET_AFTER="$(curl -fsS "$BASE/api/wallet" "${HDR[@]}" | jq -c '{address,balance_hmc}')"
echo ""
echo "[bc5] wallet after: $WALLET_AFTER"
echo "$WALLET_AFTER" >"$OUT/wallet_after_${TS}.json"

python3 - <<PY >"$OUT/summary_${TS}.json"
import json, pathlib
out = pathlib.Path("$OUT")
lines = (out / "results_${TS}.jsonl").read_text().splitlines()
rows = [json.loads(l) for l in lines if l.strip()]
summary = {
  "timestamp": "$TS",
  "base": "$BASE",
  "modules": rows,
  "all_clean": all(r.get("critical", 0) == 0 and r.get("verdict") in ("PASS", "CLEAN", "clean", "pass") for r in rows if r.get("verdict") != "error"),
  "wallet_before": json.loads(pathlib.Path("$OUT/wallet_before_${TS}.json").read_text()),
  "wallet_after": json.loads(pathlib.Path("$OUT/wallet_after_${TS}.json").read_text()),
}
print(json.dumps(summary, indent=2))
PY

echo ""
echo "[bc5] done → $OUT/summary_${TS}.json"
jq . "$OUT/summary_${TS}.json"
