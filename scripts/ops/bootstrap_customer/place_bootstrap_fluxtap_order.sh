#!/usr/bin/env bash
# Bootstrap pool order — FluxTap-class display filter (FounderB/FluxTap filter.go panic class).
# Uses guard_pack filter_utf8 + rust_fluxtap_filter_bytes_guard.wasm (seeds include \xc7=).
#
#   BOOTSTRAP_VPS_PASS='...' bash scripts/ops/bootstrap_customer/place_bootstrap_fluxtap_order.sh
#   BOOTSTRAP_DRY_RUN=1 ...  # wallet + payload only
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="${LOG_DIR:-$INSTALL/logs/bootstrap/orders}"
BUDGET_HMC="${BUDGET_HMC:-6}"
BUDGET_RUNS="${BUDGET_RUNS:-384}"
BUDGET_SEC="${BUDGET_SEC:-14400}"
POLL_SEC="${POLL_SEC:-45}"
MAX_WAIT="${MAX_WAIT:-900}"

mkdir -p "$LOG_DIR"
ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"

WASM=""
for cand in \
  "$INSTALL/tasks/artifacts/security/rust_fluxtap_filter_bytes_guard.wasm" \
  "$ROOT/tasks/artifacts/security/rust_fluxtap_filter_bytes_guard.wasm" \
  "/opt/hackme/tasks/artifacts/security/rust_fluxtap_filter_bytes_guard.wasm"; do
  if [[ -f "$cand" ]]; then WASM="$cand"; break; fi
done
[[ -n "$WASM" && -f "$WASM" ]] || { echo "[fluxtap-order] missing rust_fluxtap_filter_bytes_guard.wasm" >&2; exit 1; }
WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"

OID="order-bootstrap-fluxtap-${STAMP}"
CID="campaign-bootstrap-fluxtap-${STAMP}"
TITLE="HackMe Bootstrap Audit · FluxTap display filter · FounderB/FluxTap"

log() { echo "[fluxtap-order $(date -u +%H:%M:%S)] $*" | tee -a "$LOG_DIR/${STAMP}.log"; }

if [[ "${BOOTSTRAP_DRY_RUN:-0}" == "1" ]]; then
  jq -nc \
    --arg title "$TITLE" \
    --arg oid "$OID" \
    --arg cid "$CID" \
    --argjson budget "$BUDGET_HMC" \
    --argjson runs "$BUDGET_RUNS" \
    '{title:$title,order_id:$oid,campaign_id:$cid,guard_pack:"filter_utf8",depth_tier:"wasm_only",budget_hmc:$budget,budget_runs:$runs,pool_distributed:true,create_poh_order:false}'
  exit 0
fi

log "POST security-audit FluxTap filter_utf8 budget_hmc=$BUDGET_HMC runs=$BUDGET_RUNS"
http_code="$(curl -sS --max-time 120 -o "$LOG_DIR/${STAMP}-audit.raw.json" -w '%{http_code}' -X POST "$BASE/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$(jq -nc \
    --arg title "$TITLE" \
    --arg payer "bootstrap:hackme:89.150.41.40" \
    --arg oid "$OID" \
    --arg cid "$CID" \
    --arg hex "$WASM_HEX" \
    --argjson budget "$BUDGET_HMC" \
    --argjson runs "$BUDGET_RUNS" \
    --argjson bsec "$BUDGET_SEC" \
    '{
      title: $title,
      payer_ref: $payer,
      order_id: $oid,
      campaign_id: $cid,
      wasm_check_hex: $hex,
      guard_pack: "filter_utf8",
      guard_name: "filter_utf8",
      depth_tier: "wasm_only",
      input_mode: "bytes",
      max_input_bytes: 1024,
      budget_hmc: $budget,
      budget_runs: $runs,
      budget_seconds: $bsec,
      mutation_rounds: 2,
      guided_scheduling: true,
      seed_byte_corpus: ["c73d", "3d", "213d", "646e73", "7463702e706f7274203d3d20343333"],
      pool_distributed: true,
      create_poh_order: false,
      use_sup_discount: false
    }')")"
resp="$(cat "$LOG_DIR/${STAMP}-audit.raw.json")"
echo "$resp" | jq . >"$LOG_DIR/${STAMP}-audit.json" 2>/dev/null || cp "$LOG_DIR/${STAMP}-audit.raw.json" "$LOG_DIR/${STAMP}-audit.json"
if [[ "$http_code" != "200" ]]; then
  log "FAIL HTTP $http_code: $(jq -c . "$LOG_DIR/${STAMP}-audit.json" 2>/dev/null || cat "$LOG_DIR/${STAMP}-audit.raw.json")"
  exit 1
fi

CID_OUT="$(jq -r '.campaign.id // .campaign_id // empty' <<<"$resp")"
TOK="$(jq -r '.customer_report_token // empty' <<<"$resp")"
[[ -n "$CID_OUT" ]] || { log "FAIL no campaign id"; exit 1; }
log "created campaign=$CID_OUT pool_sync=$(jq -r '.pool_sync // ""' <<<"$resp")"

if [[ "$(jq -r '.pool_sync // ""' <<<"$resp")" == "queued" ]]; then
  log "pool_sync queued — resync"
  export BOOTSTRAP_INSTALL="$INSTALL" CAMPAIGN_ID="$CID_OUT"
  bash "$(dirname "$0")/bootstrap_resync_pool.sh" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true
fi

deadline=$(( $(date +%s) + MAX_WAIT ))
runs_done=0
findings=0
while [[ $(date +%s) -lt $deadline ]]; do
  sleep "$POLL_SEC"
  prog="$(curl -fsS --max-time 30 "$COORD/api/fuzz/pool/campaigns/progress?id=${CID_OUT}" 2>/dev/null || echo '{}')"
  runs_done="$(jq -r '.runs_done // 0' <<<"$prog")"
  findings="$(jq -r '.findings // 0' <<<"$prog")"
  status="$(jq -r '.status // ""' <<<"$prog")"
  log "progress runs_done=$runs_done findings=$findings status=$status"
  if [[ "$status" == "completed" || "$status" == "cancelled" ]]; then break; fi
  if [[ "${runs_done:-0}" -ge 8 && "${findings:-0}" -ge 1 ]]; then
    log "early OK — findings detected"
    break
  fi
done

if [[ -n "$TOK" ]]; then
  curl -fsS --max-time 60 "$BASE/api/fuzz/campaigns/${CID_OUT}/report?format=json&limit=20" \
    -H "X-Hackme-Report-Token: $TOK" | jq '{verdict,raw_findings_total,findings:(.findings|length),sample:(.findings[0:3]|map({title,severity,input_preview}))}' \
    >"$LOG_DIR/${STAMP}-report.json" 2>/dev/null || true
fi

verdict="$(jq -r '.verdict // "?"' "$LOG_DIR/${STAMP}-report.json" 2>/dev/null || echo '?')"
log "DONE campaign=$CID_OUT runs_done=$runs_done findings=$findings verdict=$verdict"
jq -nc --arg cid "$CID_OUT" --arg stamp "$STAMP" --argjson runs "$runs_done" --argjson findings "${findings:-0}" --arg verdict "$verdict" \
  '{target:"fluxtap",campaign_id:$cid,stamp:$stamp,runs_done:$runs,pool_findings:$findings,verdict:$verdict,repo:"https://github.com/FounderB/FluxTap"}' \
  >>"$INSTALL/logs/bootstrap/order_results.jsonl"
