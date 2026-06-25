#!/usr/bin/env bash
# Bitcoin Core 30-day fuzz — one upstream WASM module per day, live security-audit + report.
#
#   DAY=1 bash scripts/ops/run_bitcoin30_day.sh
#   DAY=2 CHECK_SEMANTICS=pow_gate bash scripts/ops/run_bitcoin30_day.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

DAY="${DAY:-1}"
BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(tr -d '\r\n' <"${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}" 2>/dev/null || true)"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%S)Z-${RANDOM}}"
OUT="${OUT:-$ROOT/reports/bitcoin30/day$(printf '%02d' "$DAY")-$STAMP}"
CHECK_SEMANTICS="${CHECK_SEMANTICS:-pow_gate}"
BUDGET_RUNS="${BUDGET_RUNS:-64}"
[[ "$DAY" == "6" && "${BUDGET_RUNS}" == "64" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=128
BUDGET_HMC="${BUDGET_HMC:-0.5}"
DEPTH_TIER="${DEPTH_TIER:-}"
GUARD_NAME="${GUARD_NAME:-}"
WEEK3=$(( DAY >= 15 && DAY <= 21 ))
WEEK4=$(( DAY >= 22 && DAY <= 30 ))
if [[ -z "$DEPTH_TIER" ]]; then
  if [[ "$WEEK3" == "1" ]]; then
    DEPTH_TIER=wasm_native
    [[ -z "${BUDGET_HMC_FORCE:-}" ]] && BUDGET_HMC=5
    [[ -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=256
  fi
  if [[ "$WEEK4" == "1" ]]; then
    DEPTH_TIER=bytes_corpus
    [[ -z "${BUDGET_HMC_FORCE:-}" ]] && BUDGET_HMC=10
    [[ -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=512
  fi
fi

require_cmd curl jq xxd go

[[ -n "$ADMIN" ]] || fail "missing admin token"
curl -fsS --max-time 10 "${BASE}/api/status?lite=1" >/dev/null || fail "node down at $BASE"

mkdir -p "$OUT"
log() { echo "[btc30-d${DAY}] $*" | tee -a "$OUT/run.log"; }

# day|wasm_file|guard_id|title|core_ref|hackme_src
case "$DAY" in
  1) WASM_REL="upstream_bitcoin_getscriptop.wasm"; GUARD="upstream_bitcoin_getscriptop"
     TITLE="Bitcoin Core GetScriptOp · MAX_SCRIPT_ELEMENT_SIZE (520 B)"
     CORE_REF="bitcoin/bitcoin src/script/script.cpp GetScriptOp · script.h MAX_SCRIPT_ELEMENT_SIZE"
     SRC="tasks/sources/security/upstream/bitcoin_getscriptop.c" ;;
  2) WASM_REL="upstream_bitcoin_hasvalidops.wasm"; GUARD="upstream_bitcoin_hasvalidops"
     TITLE="Bitcoin Core CScript::HasValidOps()"
     CORE_REF="bitcoin/bitcoin src/script/script.cpp HasValidOps()"
     SRC="tasks/sources/security/upstream/bitcoin_hasvalidops.c" ;;
  3) WASM_REL="upstream_bitcoin_tx_check.wasm"; GUARD="upstream_bitcoin_tx_check"
     TITLE="Bitcoin Core CheckTransaction · MoneyRange"
     CORE_REF="bitcoin/bitcoin src/consensus/tx_check.cpp · amount.h MoneyRange"
     SRC="tasks/sources/security/upstream/bitcoin_tx_check.c" ;;
  4) WASM_REL="upstream_bitcoin_tx_dup_inputs.wasm"; GUARD="upstream_bitcoin_tx_dup_inputs"
     TITLE="Bitcoin Core CheckTransaction · duplicate inputs (CVE-2018-17144)"
     CORE_REF="bitcoin/bitcoin src/consensus/tx_check.cpp duplicate prevout check"
     SRC="tasks/sources/security/upstream/bitcoin_tx_dup_inputs.c" ;;
  5) WASM_REL="upstream_bitcoin_evalscript_push.wasm"; GUARD="upstream_bitcoin_evalscript_push"
     TITLE="Bitcoin Core EvalScript · SCRIPT_ERR_PUSH_SIZE (520 B)"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript push size L457-L458"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_push.c" ;;
  6) WASM_REL="upstream_bitcoin_witness_stack.wasm"; GUARD="upstream_bitcoin_witness_stack"
     TITLE="Bitcoin Core SegWit witness stack · SCRIPT_ERR_PUSH_SIZE"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp VerifyWitnessProgram witness stack L1861-L1864"
     SRC="tasks/sources/security/upstream/bitcoin_witness_stack.c" ;;
  7) WASM_REL="upstream_bitcoin_evalscript_opcount.wasm"; GUARD="upstream_bitcoin_evalscript_opcount"
     TITLE="Bitcoin Core EvalScript · SCRIPT_ERR_OP_COUNT (201 ops)"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript nOpCount L462-L463 · script.h MAX_OPS_PER_SCRIPT"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_opcount.c" ;;
  8) WASM_REL="upstream_bitcoin_evalscript_stack.wasm"; GUARD="upstream_bitcoin_evalscript_stack"
     TITLE="Bitcoin Core EvalScript · SCRIPT_ERR_STACK_SIZE (1000 elements)"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript stack+altstack L334-L335 · script.h MAX_STACK_SIZE"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_stack.c" ;;
  9) WASM_REL="upstream_bitcoin_block_weight.wasm"; GUARD="upstream_bitcoin_block_weight"
     TITLE="Bitcoin Core block weight · MAX_BLOCK_WEIGHT (4M WU)"
     CORE_REF="bitcoin/bitcoin src/consensus/validation.h MAX_BLOCK_WEIGHT · GetBlockWeight"
     SRC="tasks/sources/security/upstream/bitcoin_block_weight.c" ;;
  10) WASM_REL="upstream_bitcoin_coinbase_bip34.wasm"; GUARD="upstream_bitcoin_coinbase_bip34"
     TITLE="Bitcoin Core BIP34 · coinbase must push block height"
     CORE_REF="bitcoin/bitcoin validation.cpp ConnectBlock BIP34 coinbase height"
     SRC="tasks/sources/security/upstream/bitcoin_coinbase_bip34.c"
     [[ "${BUDGET_RUNS}" == "64" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=128 ;;
  11) WASM_REL="upstream_bitcoin_tx_dup_inputs.wasm"; GUARD="upstream_bitcoin_tx_dup_inputs"
     TITLE="Bitcoin Core duplicate inputs · deep pass (CVE-2018-17144 class)"
     CORE_REF="bitcoin/bitcoin src/consensus/tx_check.cpp duplicate prevout check"
     SRC="tasks/sources/security/upstream/bitcoin_tx_dup_inputs.c"
     [[ "${BUDGET_RUNS}" == "64" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=256 ;;
  12) WASM_REL="upstream_bitcoin_evalscript_stack.wasm"; GUARD="upstream_bitcoin_evalscript_stack"
     TITLE="Bitcoin Core EvalScript · SCRIPT_ERR_STACK_SIZE · deep pass"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript stack+altstack L334-L335"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_stack.c"
     [[ "${BUDGET_RUNS}" == "64" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=256 ;;
  13) WASM_REL="upstream_bitcoin_witness_stack.wasm"; GUARD="upstream_bitcoin_witness_stack"
     TITLE="Bitcoin Core SegWit witness stack · deep pass"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp VerifyWitnessProgram witness stack"
     SRC="tasks/sources/security/upstream/bitcoin_witness_stack.c"
     [[ "${BUDGET_RUNS}" == "64" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=256 ;;
  14) WASM_REL="upstream_bitcoin_evalscript_opcount.wasm"; GUARD="upstream_bitcoin_evalscript_opcount"
     TITLE="Bitcoin Core EvalScript · SCRIPT_ERR_OP_COUNT · deep pass"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript nOpCount L462-L463"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_opcount.c"
     [[ "${BUDGET_RUNS}" == "64" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=256 ;;
  15) WASM_REL="upstream_bitcoin_tx_dup_inputs.wasm"; GUARD="upstream_bitcoin_tx_dup_inputs"
     TITLE="Bitcoin Core duplicate inputs · wasm_native + native bridge (Week 3)"
     CORE_REF="bitcoin/bitcoin src/consensus/tx_check.cpp duplicate prevout check"
     SRC="tasks/sources/security/upstream/bitcoin_tx_dup_inputs.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_tx_dup_inputs}" ;;
  16) WASM_REL="upstream_bitcoin_coinbase_bip34.wasm"; GUARD="upstream_bitcoin_coinbase_bip34"
     TITLE="Bitcoin Core BIP34 coinbase height · wasm_native (Week 3)"
     CORE_REF="bitcoin/bitcoin validation.cpp ConnectBlock BIP34 coinbase height"
     SRC="tasks/sources/security/upstream/bitcoin_coinbase_bip34.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_coinbase_bip34}" ;;
  17) WASM_REL="upstream_bitcoin_block_weight.wasm"; GUARD="upstream_bitcoin_block_weight"
     TITLE="Bitcoin Core block weight · wasm_native (Week 3)"
     CORE_REF="bitcoin/bitcoin src/consensus/validation.h MAX_BLOCK_WEIGHT"
     SRC="tasks/sources/security/upstream/bitcoin_block_weight.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_block_weight}" ;;
  18) WASM_REL="upstream_bitcoin_evalscript_push.wasm"; GUARD="upstream_bitcoin_evalscript_push"
     TITLE="Bitcoin Core EvalScript push size · wasm_native (Week 3)"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript push size"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_push.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_evalscript_push}" ;;
  19) WASM_REL="upstream_bitcoin_witness_stack.wasm"; GUARD="upstream_bitcoin_witness_stack"
     TITLE="Bitcoin Core SegWit witness stack · wasm_native deep (Week 3)"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp VerifyWitnessProgram"
     SRC="tasks/sources/security/upstream/bitcoin_witness_stack.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_witness_stack}" ;;
  20) WASM_REL="upstream_bitcoin_evalscript_stack.wasm"; GUARD="upstream_bitcoin_evalscript_stack"
     TITLE="Bitcoin Core EvalScript stack size · wasm_native deep (Week 3)"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript stack+altstack"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_stack.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_evalscript_stack}" ;;
  21) WASM_REL="upstream_bitcoin_evalscript_opcount.wasm"; GUARD="upstream_bitcoin_evalscript_opcount"
     TITLE="Bitcoin Core EvalScript op count · wasm_native · Week 3 capstone"
     CORE_REF="bitcoin/bitcoin src/script/interpreter.cpp EvalScript nOpCount"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_opcount.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_evalscript_opcount}" ;;
  22) WASM_REL="upstream_bitcoin_tx_dup_inputs.wasm"; GUARD="upstream_bitcoin_tx_dup_inputs"
     TITLE="Bitcoin Core duplicate inputs · bytes_corpus (Week 4)"
     CORE_REF="bitcoin/bitcoin src/consensus/tx_check.cpp duplicate prevout check"
     SRC="tasks/sources/security/upstream/bitcoin_tx_dup_inputs.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_tx_dup_inputs}" ;;
  23) WASM_REL="upstream_bitcoin_coinbase_bip34.wasm"; GUARD="upstream_bitcoin_coinbase_bip34"
     TITLE="Bitcoin Core BIP34 · bytes_corpus (Week 4)"
     CORE_REF="bitcoin/bitcoin validation.cpp BIP34 coinbase height"
     SRC="tasks/sources/security/upstream/bitcoin_coinbase_bip34.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_coinbase_bip34}" ;;
  24) WASM_REL="upstream_bitcoin_block_weight.wasm"; GUARD="upstream_bitcoin_block_weight"
     TITLE="Bitcoin Core block weight · bytes_corpus (Week 4)"
     CORE_REF="bitcoin/bitcoin MAX_BLOCK_WEIGHT"
     SRC="tasks/sources/security/upstream/bitcoin_block_weight.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_block_weight}" ;;
  25) WASM_REL="upstream_bitcoin_getscriptop.wasm"; GUARD="upstream_bitcoin_getscriptop"
     TITLE="Bitcoin Core GetScriptOp · bytes_corpus (Week 4)"
     CORE_REF="bitcoin/bitcoin src/script/script.cpp GetScriptOp"
     SRC="tasks/sources/security/upstream/bitcoin_getscriptop.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_getscriptop}" ;;
  26) WASM_REL="upstream_bitcoin_hasvalidops.wasm"; GUARD="upstream_bitcoin_hasvalidops"
     TITLE="Bitcoin Core HasValidOps · bytes_corpus (Week 4)"
     CORE_REF="bitcoin/bitcoin src/script/script.cpp HasValidOps"
     SRC="tasks/sources/security/upstream/bitcoin_hasvalidops.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_hasvalidops}" ;;
  27) WASM_REL="upstream_bitcoin_tx_check.wasm"; GUARD="upstream_bitcoin_tx_check"
     TITLE="Bitcoin Core CheckTransaction MoneyRange · bytes_corpus (Week 4)"
     CORE_REF="bitcoin/bitcoin src/consensus/tx_check.cpp MoneyRange"
     SRC="tasks/sources/security/upstream/bitcoin_tx_check.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_tx_check}" ;;
  28) WASM_REL="upstream_bitcoin_evalscript_push.wasm"; GUARD="upstream_bitcoin_evalscript_push"
     TITLE="Bitcoin Core EvalScript push · bytes_corpus deep (Week 4)"
     CORE_REF="bitcoin/bitcoin EvalScript push size"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_push.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_evalscript_push}"
     [[ "${BUDGET_RUNS}" == "512" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=1000 ;;
  29) WASM_REL="upstream_bitcoin_witness_stack.wasm"; GUARD="upstream_bitcoin_witness_stack"
     TITLE="Bitcoin Core witness stack · bytes_corpus capstone (Day 29)"
     CORE_REF="bitcoin/bitcoin VerifyWitnessProgram witness stack"
     SRC="tasks/sources/security/upstream/bitcoin_witness_stack.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_witness_stack}"
     [[ "${BUDGET_RUNS}" == "512" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=1000 ;;
  30) WASM_REL="upstream_bitcoin_evalscript_stack.wasm"; GUARD="upstream_bitcoin_evalscript_stack"
     TITLE="Bitcoin Core EvalScript stack · bytes_corpus · Day 30 milestone"
     CORE_REF="bitcoin/bitcoin EvalScript stack limits"
     SRC="tasks/sources/security/upstream/bitcoin_evalscript_stack.c"
     GUARD_NAME="${GUARD_NAME:-bitcoin_evalscript_stack}"
     [[ "${BUDGET_RUNS}" == "512" && -z "${BUDGET_RUNS_FORCE:-}" ]] && BUDGET_RUNS=1000 ;;
  *) fail "DAY=$DAY not in schedule (1-30)" ;;
esac

WASM="$ROOT/tasks/artifacts/security/$WASM_REL"
log "build upstream WASM pack"
bash "$ROOT/scripts/build_upstream_l1_pack.sh" >>"$OUT/build.log" 2>&1
[[ -f "$WASM" ]] || fail "missing $WASM"
go run ./tools/task_abi_check "$WASM" >>"$OUT/build.log" 2>&1

WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"
CID="campaign-btc30-d${DAY}-${STAMP}"
OID="order-btc30-d${DAY}-${STAMP}"

log "module: $TITLE"
log "wasm: $WASM ($(wc -c <"$WASM") bytes)"

curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "${BASE}/api/wallet" \
  | jq '{address,balance_hmc}' | tee "$OUT/wallet_before.json"

log "POST /api/security-audit ($CHECK_SEMANTICS depth_tier=${DEPTH_TIER:-wasm_only})"
AUDIT_JSON="$(jq -nc \
  --arg title "$TITLE" \
  --arg payer "bitcoin30:day${DAY}" \
  --arg oid "$OID" \
  --arg cid "$CID" \
  --arg hex "$WASM_HEX" \
  --arg sem "$CHECK_SEMANTICS" \
  --arg tier "${DEPTH_TIER:-}" \
  --arg gname "${GUARD_NAME:-}" \
  --argjson runs "$BUDGET_RUNS" \
  --argjson budget "$BUDGET_HMC" \
  '{
    title: $title,
    payer_ref: $payer,
    order_id: $oid,
    campaign_id: $cid,
    wasm_check_hex: $hex,
    budget_hmc: $budget,
    budget_runs: $runs,
    budget_seconds: 1800,
    create_poh_order: true,
    pool_distributed: false,
    check_semantics: $sem,
    difficulty_score: 10,
    target_solves: 1,
    reward_hmc: 0.05
  }
  | if ($tier | length) > 0 then . + {depth_tier: $tier} else . end
  | if ($gname | length) > 0 then . + {guard_name: $gname} else . end')"
curl -fsS --max-time 120 -X POST "${BASE}/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$AUDIT_JSON" | tee "$OUT/audit_create.json" || {
  log "audit POST failed — retry after 5s (SQLITE_BUSY)"
  sleep 5
  curl -fsS --max-time 120 -X POST "${BASE}/api/security-audit" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN" \
    -d "$AUDIT_JSON" | tee "$OUT/audit_create.json"
}

REPORT_TOKEN="$(jq -r '.customer_report_token // empty' "$OUT/audit_create.json")"
[[ -n "$REPORT_TOKEN" ]] || fail "no customer_report_token"
echo "$REPORT_TOKEN" >"$OUT/report_token.txt"

log "poll campaign (max 180s)"
for i in $(seq 1 90); do
  curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" \
    "${BASE}/api/fuzz/campaigns/${CID}" >"$OUT/campaign_poll.json"
  st="$(jq -r '.campaign.status // "?"' "$OUT/campaign_poll.json")"
  done_n="$(jq -r '.campaign.summary.runs_done // 0' "$OUT/campaign_poll.json")"
  bud="$(jq -r '.campaign.budget_runs // 0' "$OUT/campaign_poll.json")"
  log "  tick $i status=$st runs=$done_n/$bud"
  [[ "$st" == "completed" || "$st" == "failed" ]] && break
  sleep 2
done

curl -fsS --max-time 30 -H "X-Hackme-Report-Token: $REPORT_TOKEN" \
  "${BASE}/api/fuzz/campaigns/${CID}/report?format=json&limit=80" \
  | jq . | tee "$OUT/report.json"

curl -fsS --max-time 30 -H "X-Hackme-Report-Token: $REPORT_TOKEN" \
  "${BASE}/api/fuzz/campaigns/${CID}/report.html?limit=80" \
  -o "$OUT/report.html"

curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" "${BASE}/api/wallet" \
  | jq '{address,balance_hmc}' | tee "$OUT/wallet_after.json"

python3 - "$OUT" "$DAY" "$GUARD" "$TITLE" "$CORE_REF" "$SRC" "$CID" "$OID" "$CHECK_SEMANTICS" <<'PY'
import json, pathlib, sys
out, day, guard, title, core, src, cid, oid, sem = sys.argv[1:10]
out = pathlib.Path(out)
rep = json.loads((out / "report.json").read_text())
camp = rep.get("campaign") or {}
cs = camp.get("summary") or {}
native = cs.get("native") or {}
tot = rep.get("totals") or {}
by_sev = tot.get("by_severity") or {}
findings = rep.get("findings") or []
guard_n = sum(1 for f in findings if (f.get("detail") or {}).get("triage_class") == "guard_signal")
started = camp.get("started_at") or 0
completed = camp.get("completed_at") or 0
cfg = camp.get("config") or {}
summary = {
    "day": int(day),
    "guard": guard,
    "title": title,
    "bitcoin_core": core,
    "hackme_source": src,
    "campaign_id": cid,
    "order_id": oid,
    "check_semantics": cfg.get("check_semantics") or sem,
    "depth_tier": cfg.get("depth_tier"),
    "runs_done": cs.get("runs_done"),
    "verdict": rep.get("verdict"),
    "critical_count": by_sev.get("critical", 0),
    "high_count": by_sev.get("high", 0),
    "guard_signal_count": guard_n,
    "native_status": native.get("status"),
    "native_confirmed": native.get("confirmed_count", 0),
    "native_rejected": native.get("rejected_count", 0),
    "new_edges": cs.get("new_edges", 0),
    "new_paths": cs.get("new_paths", 0),
    "unique_crashes": cs.get("unique_crashes", 0),
    "duration_sec": max(0, completed - started) if started and completed else None,
    "time_to_first_crash_sec": cs.get("time_to_first_crash_sec"),
    "sample_repro": [f.get("repro_cmd") for f in findings[:2] if f.get("repro_cmd")],
}
(out / "DAY_SUMMARY.json").write_text(json.dumps(summary, indent=2))
print(json.dumps(summary, indent=2))
PY

ln -sfn "$(basename "$OUT")" "$ROOT/reports/bitcoin30/CURRENT"
log "done → $OUT/DAY_SUMMARY.json + report.html (CURRENT → $(basename "$OUT"))"
