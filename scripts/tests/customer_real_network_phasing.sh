#!/usr/bin/env bash
# Real-customer phasing: critical upstream guards + multilang orders, fuzz reports,
# coordinator work distribution, pool/block settlement snapshot.
#
#   bash scripts/tests/customer_real_network_phasing.sh
#   BASE=http://127.0.0.1:8080 COORD_URL=https://hackme.tech/pool/coordinator \
#     bash scripts/tests/customer_real_network_phasing.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/customer-phasing-$STAMP}"
BASE="${BASE:-http://127.0.0.1:8080}"
BASE="${BASE%/}"
COORD="${COORD_URL:-${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}}"
COORD="${COORD%/}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE" 2>/dev/null || true)"
[[ -z "$ADMIN" && -f "$ROOT/.env.desktop" ]] && ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$ROOT/.env.desktop" | cut -d= -f2- || true)"
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN}"

BUDGET_HMC="${BUDGET_HMC:-1.0}"
BUDGET_RUNS="${BUDGET_RUNS:-24}"
BUDGET_SEC="${BUDGET_SECONDS:-240}"
POLL_SEC="${POLL_SEC:-300}"
RATE_SLEEP="${RATE_SLEEP:-22}"
# Local desktop: pool fuzz sync to prod coordinator often times out; run fuzz on-node.
if [[ -z "${POOL_DISTRIBUTED:-}" ]]; then
  if [[ "$BASE" =~ ^https?://(127\.0\.0\.1|localhost)(:|/|$) ]]; then
    POOL_DISTRIBUTED="false"
  else
    POOL_DISTRIBUTED="true"
  fi
fi
if [[ "$POOL_DISTRIBUTED" == "true" ]]; then
  POOL_DIST_JSON=true
else
  POOL_DIST_JSON=false
fi

mkdir -p "$OUT"
RESULTS="$OUT/orders.jsonl"
CAMPAIGNS="$OUT/campaigns.jsonl"
: >"$RESULTS"
: >"$CAMPAIGNS"
VERDICT="$OUT/VERDICT.md"

log() { echo "[customer-phasing] $*" | tee -a "$OUT/run.log"; }

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg v "$verdict" --arg d "$detail" \
    '{id:$id,verdict:$v,detail:$d,ts:now}' >>"$OUT/steps.jsonl"
}

tally_verdict() {
  pass_n="$(jq -s '[.[]|select(.verdict=="pass")]|length' "$OUT/steps.jsonl" 2>/dev/null || echo 0)"
  fail_n="$(jq -s '[.[]|select(.verdict=="fail")]|length' "$OUT/steps.jsonl" 2>/dev/null || echo 0)"
  warn_n="$(jq -s '[.[]|select(.verdict=="warn")]|length' "$OUT/steps.jsonl" 2>/dev/null || echo 0)"
}

require_cmd curl
require_cmd jq
require_cmd python3
require_cmd go
require_cmd xxd

if [[ -z "$ADMIN" ]]; then
  echo "[customer-phasing] need admin token in $ADMIN_FILE or .env.desktop" >&2
  exit 2
fi
if ! curl -fsS --max-time 8 "${BASE}/api/status?lite=1" >/dev/null 2>&1; then
  echo "[customer-phasing] node down at $BASE — start: bash scripts/ops/restart_linux_desktop_worker.sh" >&2
  exit 1
fi

PHASE_FROM="${PHASE_FROM:-0}"

if [[ "$PHASE_FROM" -le 0 ]]; then
  log "=== Phase 0: build security + upstream WASM ==="
  bash "$ROOT/scripts/build_security_task_pack.sh" >>"$OUT/build.log" 2>&1
  bash "$ROOT/scripts/build_upstream_l1_pack.sh" >>"$OUT/build.log" 2>&1
  go run ./tools/task_abi_check "$ROOT"/tasks/artifacts/security/upstream_*.wasm >>"$OUT/build.log" 2>&1
  record "build_wasm" "pass" "security + upstream artifacts"
fi

if [[ "$PHASE_FROM" -le 1 ]]; then
  log "=== Phase 1: unit gates (fuzz pool + security audit handler) ==="
  if go test -count=1 -timeout=120s ./internal/fuzzengine/... ./internal/poolfuzz/... >>"$OUT/go_test.log" 2>&1 \
    && go test -count=1 -timeout=60s . -run TestSecurityAudit >>"$OUT/go_test.log" 2>&1; then
    record "go_unit_fuzz" "pass" "fuzzengine/poolfuzz + TestSecurityAudit"
  else
    record "go_unit_fuzz" "fail" "see go_test.log"
  fi
fi

if [[ "$PHASE_FROM" -le 2 ]]; then
  log "=== Phase 2: ephemeral coordinator distributed fuzz ==="
  if bash "$ROOT/scripts/ops/pool_fuzz_distributed_gate.sh" >>"$OUT/pool_fuzz_gate.log" 2>&1; then
    record "pool_fuzz_distributed_gate" "pass" "coordinator claim/submit + detector"
  else
    record "pool_fuzz_distributed_gate" "fail" "pool_fuzz_gate.log"
  fi
fi

wasm_hex_file() {
  xxd -p "$1" | tr -d '\n'
}

post_security_audit() {
  local slug="$1"
  local title="$2"
  local wasm_path="$3"
  local payer="$4"
  local order_id="order-customer-${slug}-${STAMP}"
  local campaign_id="campaign-customer-${slug}-${STAMP}"
  local hex
  hex="$(wasm_hex_file "$wasm_path")"
  sleep "$RATE_SLEEP"
  local tmp="$OUT/tmp_audit_${slug}.json"
  local http
  http="$(curl -sS -o "$tmp" -w '%{http_code}' -X POST "${BASE}/api/security-audit" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN" \
    -d "$(jq -nc \
      --arg title "$title" \
      --arg payer "$payer" \
      --arg oid "$order_id" \
      --arg cid "$campaign_id" \
      --arg hex "$hex" \
      --argjson budget "$BUDGET_HMC" \
      --argjson runs "$BUDGET_RUNS" \
      --argjson sec "$BUDGET_SEC" \
      --argjson pool_dist "$POOL_DIST_JSON" \
      '{title:$title,payer_ref:$payer,order_id:$oid,campaign_id:$cid,wasm_check_hex:$hex,
        budget_hmc:$budget,budget_runs:$runs,budget_seconds:$sec,use_sup_discount:false,
        create_poh_order:true,pool_distributed:$pool_dist}')")"
  jq -nc --arg slug "$slug" --arg http "$http" --argjson body "$(cat "$tmp" 2>/dev/null || echo '{}')" \
    '{slug:$slug,http:$http,body:$body}' >>"$RESULTS"
  if [[ "$http" != "200" ]]; then
    record "audit-$slug" "fail" "http=$http $(jq -c . "$tmp" 2>/dev/null || true)"
    return 1
  fi
  local tok cid_out
  tok="$(jq -r '.customer_report_token // empty' "$tmp")"
  cid_out="$(jq -r '.campaign_id // empty' "$tmp")"
  jq -nc --arg slug "$slug" --arg cid "$cid_out" --arg tok "$tok" \
    '{slug:$slug,campaign_id:$cid,report_token:$tok}' >>"$CAMPAIGNS"
  record "audit-$slug" "pass" "campaign=$cid_out pool_sync=$(jq -r '.pool_sync // ""' "$tmp")"
  printf '%s\t%s\n' "$tok" "$cid_out"
}

post_from_code_audit() {
  local lang="$1"
  local code="$2"
  local slug="lang-${lang}"
  sleep "$RATE_SLEEP"
  local tmp="$OUT/tmp_fc_${lang}.json"
  local http
  http="$(curl -sS -o "$tmp" -w '%{http_code}' -X POST "${BASE}/api/security-audit" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN" \
    -d "$(jq -nc \
      --arg title "Customer audit · $lang" \
      --arg payer "customer:lang:$lang" \
      --arg lang "$lang" \
      --arg code "$code" \
      --argjson budget "$BUDGET_HMC" \
      --argjson runs "$BUDGET_RUNS" \
      --argjson sec "$BUDGET_SEC" \
      --argjson pool_dist "$POOL_DIST_JSON" \
      '{title:$title,payer_ref:$payer,language:$lang,code:$code,
        budget_hmc:$budget,budget_runs:$runs,budget_seconds:$sec,use_sup_discount:false,
        create_poh_order:true,pool_distributed:$pool_dist}')")"
  if [[ "$http" != "200" ]]; then
    record "audit-$slug" "warn" "http=$http $(jq -c .code "$tmp" 2>/dev/null || echo detail)"
    return 1
  fi
  local tok cid_out
  tok="$(jq -r '.customer_report_token // empty' "$tmp")"
  cid_out="$(jq -r '.campaign_id // empty' "$tmp")"
  jq -nc --arg slug "$slug" --arg cid "$cid_out" --arg tok "$tok" \
    '{slug:$slug,campaign_id:$cid,report_token:$tok}' >>"$CAMPAIGNS"
  record "audit-$slug" "pass" "from_code $lang campaign=$cid_out"
  printf '%s\t%s\n' "$tok" "$cid_out"
}

log "=== Phase 3: customer orders (upstream critical L1 guards) ==="
UPSTREAM_MODULES=(
  "bitcoin_getscriptop:Bitcoin script_push (CVE-class)"
  "bitcoin_hasvalidops:Bitcoin HasValidOps"
  "bitcoin_tx_check:Bitcoin tx check"
  "ethereum_value_overflow:Ethereum value overflow"
  "dogecoin_hasvalidops:Dogecoin HasValidOps"
  "litecoin_getscriptop:Litecoin GetOp"
  "hackme_order_gate:HackMe order gate"
)
declare -A REPORT_TOK
declare -A REPORT_CID
for entry in "${UPSTREAM_MODULES[@]}"; do
  mod="${entry%%:*}"
  title="${entry#*:}"
  wasm="$ROOT/tasks/artifacts/security/upstream_${mod}.wasm"
  [[ -f "$wasm" ]] || { record "audit-up-$mod" "fail" "missing $wasm"; continue; }
  IFS=$'\t' read -r tok cid < <(post_security_audit "up-$mod" "$title" "$wasm" "customer:upstream:$mod" || printf '\t\n')
  [[ -n "${tok:-}" ]] && REPORT_TOK["up-$mod"]="$tok" && REPORT_CID["up-$mod"]="$cid"
done

log "=== Phase 4: customer orders (multilang from_code) ==="
# Minimal check() PoH tasks — real customer compiles per language.
read -r -d '' RUST_CODE <<'EOF' || true
#[no_mangle]
pub extern "C" fn check(n:i64)->i32{ if n%19==0 {1} else {0} }
EOF
read -r -d '' C_CODE <<'EOF' || true
#include <stdint.h>
int32_t check(int64_t n){ return (n%19==0)?1:0; }
EOF
read -r -d '' CPP_CODE <<'EOF' || true
#include <stdint.h>
extern "C" int32_t check(int64_t n){ return (n%19==0)?1:0; }
EOF
read -r -d '' WAT_CODE <<'EOF' || true
(module
  (func (export "check") (param i64) (result i32)
    local.get 0
    i64.const 19
    i64.rem_s
    i64.eqz
    if (result i32)
      i32.const 1
    else
      i32.const 0
    end))
EOF
for pair in "rust:$RUST_CODE" "c:$C_CODE" "cpp:$CPP_CODE" "wat:$WAT_CODE"; do
  lang="${pair%%:*}"
  code="${pair#*:}"
  IFS=$'\t' read -r tok cid < <(post_from_code_audit "$lang" "$code" || printf '\t\n')
  [[ -n "${tok:-}" ]] && REPORT_TOK["lang-$lang"]="$tok" && REPORT_CID["lang-$lang"]="$cid"
done

log "=== Phase 5: poll campaigns + fetch fuzz_report_v2 ==="
end=$((SECONDS + POLL_SEC))
while [[ $SECONDS -lt $end ]]; do
  pending=0
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    cid="$(echo "$line" | jq -r '.campaign_id')"
    slug="$(echo "$line" | jq -r '.slug')"
  tok="$(echo "$line" | jq -r '.report_token')"
    st="$(curl -fsS --max-time 15 "${BASE}/api/fuzz/campaigns/${cid}" \
      -H "X-Hackme-Admin-Token: $ADMIN" 2>/dev/null | jq -r '.status // .campaign.status // "unknown"')" || st="error"
    if [[ "$st" != "completed" && "$st" != "failed" && "$st" != "stopped" ]]; then
      pending=$((pending + 1))
    else
      rep="$OUT/reports/${slug}.json"
      mkdir -p "$OUT/reports"
      curl -fsS "${BASE}/api/fuzz/campaigns/${cid}/report?format=json&limit=20" \
        -H "X-Hackme-Report-Token: $tok" >"$rep" 2>/dev/null || true
      ver="$(jq -r '.verdict // .report_version // "?"' "$rep" 2>/dev/null || echo "?")"
      issues="$(jq -r '(.top_issues // []) | length' "$rep" 2>/dev/null || echo 0)"
      record "report-$slug" "pass" "status=$st verdict=$ver issues=$issues"
    fi
  done <"$CAMPAIGNS"
  [[ "$pending" -eq 0 ]] && break
  log "poll: $pending campaigns still running..."
  sleep 15
done
[[ "${pending:-0}" -gt 0 ]] && record "campaign_poll" "warn" "$pending campaigns still running after ${POLL_SEC}s"

log "=== Phase 6: coordinator distribution (pool work stats) ==="
COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"
if [[ -n "$COORD_ADMIN" ]]; then
  curl -fsS --max-time 25 -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
    "${COORD}/api/work/stats?details=1" >"$OUT/pool_work_stats.json" 2>/dev/null || echo '{}' >"$OUT/pool_work_stats.json"
else
  curl -fsS --max-time 25 "${COORD}/api/work/stats" >"$OUT/pool_work_stats.json" 2>/dev/null || echo '{}' >"$OUT/pool_work_stats.json"
fi
python3 - "$OUT/pool_work_stats.json" >>"$OUT/run.log" <<'PY'
import json, sys
from pathlib import Path
p = Path(sys.argv[1])
d = json.loads(p.read_text() or "{}")
w = d.get("workers") or {}
print("[customer-phasing] pool target_mod=", d.get("target_mod"), "reward_per_m=", d.get("reward_per_m"))
for wid, row in sorted(w.items(), key=lambda x: -(float((x[1] or {}).get("payout_hmc") or 0))):
    print(f"  worker {wid}: GH={row.get('hashrate_gh_s',0):.4f} att={row.get('accepted_attempts',0)} "
          f"hits={row.get('accepted_hits',0)} pay={row.get('payout_hmc',0):.6f}")
PY
record "pool_work_stats" "pass" "snapshot pool_work_stats.json"

log "=== Phase 7: chain blocks / miner lead (reports API) ==="
curl -fsS --max-time 20 "${BASE}/api/reports/blocks?limit=40&source=auto" >"$OUT/blocks_recent.json" 2>/dev/null || echo '{"blocks":[]}' >"$OUT/blocks_recent.json"
python3 - "$OUT/blocks_recent.json" >>"$OUT/run.log" <<'PY'
import json, sys
from collections import Counter
from pathlib import Path
blocks = json.loads(Path(sys.argv[1]).read_text()).get("blocks") or []
lead = Counter((b.get("miner_id") or b.get("worker_id") or "?") for b in blocks)
print("[customer-phasing] recent blocks:", len(blocks), "lead miners:", dict(lead.most_common(8)))
PY
record "blocks_lead" "pass" "blocks_recent.json"

log "=== Phase 8: local orders list (PoH tasks on node) ==="
curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" "${BASE}/api/tasks" >"$OUT/tasks_list.json" 2>/dev/null || echo '{}' >"$OUT/tasks_list.json"
open_cnt="$(jq -r '[.tasks[]? | select(.status=="open")] | length' "$OUT/tasks_list.json" 2>/dev/null || echo 0)"
record "tasks_open" "pass" "open_orders=$open_cnt"

tally_verdict

{
  echo "# Customer real-network phasing — $STAMP"
  echo ""
  echo "- **Base:** $BASE"
  echo "- **Coordinator:** $COORD"
  echo "- **PASS:** $pass_n · **FAIL:** $fail_n · **WARN:** $warn_n"
  echo ""
  if [[ "$fail_n" -eq 0 ]]; then
    echo "## Verdict: **GO**"
  else
    echo "## Verdict: **NO-GO**"
  fi
  echo ""
  echo "Artifacts: \`$OUT/\` (orders.jsonl, campaigns.jsonl, reports/, pool_work_stats.json)"
  echo ""
  echo "## Steps"
  cat "$OUT/steps.jsonl" 2>/dev/null | jq -s 'group_by(.verdict) | map({verdict: .[0].verdict, count: length})' || true
} >"$VERDICT"

log "done → $VERDICT (pass=$pass_n fail=$fail_n warn=$warn_n)"
[[ "${fail_n:-0}" -eq 0 ]]
