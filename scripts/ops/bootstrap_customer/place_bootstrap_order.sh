#!/usr/bin/env bash
# Place one HackMe Bootstrap Audit order (pool-distributed deep fuzz + PoH attach).
# create_poh_order=true with pool_distributed=true does NOT escrow PoH on the customer
# node — coordinator attaches the PoH order on ORDERS_URL so workerpoh fleets auto-switch.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
TARGET="${1:-nghttp2}"
BUDGET_HMC="${BUDGET_HMC:-10}"
BUDGET_RUNS="${BUDGET_RUNS:-384}"
REWARD_HMC="${REWARD_HMC:-0.05}"
TARGET_SOLVES="${TARGET_SOLVES:-8}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="${LOG_DIR:-$INSTALL/logs/bootstrap/orders}"
mkdir -p "$LOG_DIR"

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
WASM="${WASM_FILE:-$INSTALL/tasks/artifacts/security/rust_script_push_bounds_guard.wasm}"
[[ -f "$WASM" ]] || WASM="$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm"
[[ -f "$WASM" ]] || { echo "[bootstrap-order] missing wasm: $WASM" >&2; exit 1; }

WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"
OID="order-bootstrap-${TARGET}-${STAMP}"
CID="campaign-bootstrap-${TARGET}-${STAMP}"
TITLE="HackMe Bootstrap Audit · ${TARGET} · deep pool"

log() { echo "[bootstrap-order $(date -u +%H:%M:%S)] $*" | tee -a "$LOG_DIR/${STAMP}.log"; }

export BOOTSTRAP_INSTALL="$INSTALL" SNAP_DIR="$INSTALL/logs/bootstrap/snapshots"
export CAMPAIGN_ID=""
bash "$(dirname "$0")/bootstrap_snapshot.sh" "$STAMP" "pre-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true

log "POST security-audit target=$TARGET budget_hmc=$BUDGET_HMC runs=$BUDGET_RUNS poh_reward=$REWARD_HMC solves=$TARGET_SOLVES"
resp="$(curl -fsS --max-time 120 -X POST "$BASE/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$(jq -nc \
    --arg title "$TITLE" \
    --arg payer "bootstrap:hackme:89.150.41.40" \
    --arg oid "$OID" \
    --arg cid "$CID" \
    --arg hex "$WASM_HEX" \
    --arg target "$TARGET" \
    --argjson budget "$BUDGET_HMC" \
    --argjson runs "$BUDGET_RUNS" \
    --argjson reward "$REWARD_HMC" \
    --argjson solves "$TARGET_SOLVES" \
    '{
      title: $title,
      payer_ref: $payer,
      order_id: $oid,
      campaign_id: $cid,
      wasm_check_hex: $hex,
      budget_hmc: $budget,
      budget_runs: $runs,
      budget_seconds: 14400,
      depth_tier: "bytes_corpus",
      guard_name: ("upstream_" + $target),
      create_poh_order: true,
      reward_hmc: $reward,
      target_solves: $solves,
      difficulty_score: 10,
      pool_distributed: true,
      use_sup_discount: false
    }')")"

echo "$resp" | jq . >"$LOG_DIR/${STAMP}-audit.json"
TOK="$(jq -r '.customer_report_token // empty' <<<"$resp")"
CID_OUT="$(jq -r '.campaign_id // empty' <<<"$resp")"
[[ -n "$CID_OUT" ]] || { log "FAIL create: $(cat "$LOG_DIR/${STAMP}-audit.json")"; exit 1; }
log "created campaign=$CID_OUT order=$(jq -c '.order // {}' <<<"$resp") pool_sync=$(jq -r '.pool_sync // ""' <<<"$resp")"

if [[ "$(jq -r '.pool_sync // ""' <<<"$resp")" == "queued" ]]; then
  log "pool_sync queued — pushing to coordinator"
  CAMPAIGN_ID="$CID_OUT" bash "$(dirname "$0")/bootstrap_resync_pool.sh" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true
fi

export CAMPAIGN_ID="$CID_OUT"
bash "$(dirname "$0")/bootstrap_snapshot.sh" "$STAMP" "post-create-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true

POLL_SEC="${POLL_SEC:-120}"
MAX_WAIT="${MAX_WAIT:-7200}"
deadline=$(( $(date +%s) + MAX_WAIT ))
runs_done=0
while [[ $(date +%s) -lt $deadline ]]; do
  sleep "$POLL_SEC"
  prog="$(curl -fsS --max-time 30 "$COORD/api/fuzz/pool/campaigns/progress?id=${CID_OUT}" 2>/dev/null || echo '{}')"
  runs_done="$(jq -r '.runs_done // 0' <<<"$prog")"
  status="$(jq -r '.status // ""' <<<"$prog")"
  work="$(curl -fsS --max-time 15 "$COORD/api/work/stats" 2>/dev/null || echo '{}')"
  log "progress runs_done=$runs_done status=$status scheduler=$(jq -r '.scheduler_mode // "?"' <<<"$work") orders_active=$(jq -r '.orders_active // false' <<<"$work")"
  export CAMPAIGN_ID="$CID_OUT"
  bash "$(dirname "$0")/bootstrap_snapshot.sh" "${STAMP}-p$(date +%s)" "poll-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true
  if [[ "$status" == "completed" || "$status" == "cancelled" ]]; then
    break
  fi
  if [[ "${runs_done:-0}" -ge "$BUDGET_RUNS" ]]; then
    break
  fi
done

if [[ -n "$TOK" ]]; then
  curl -fsS --max-time 60 "$BASE/api/fuzz/campaigns/${CID_OUT}/report?format=json&limit=30" \
    -H "X-Hackme-Report-Token: $TOK" | jq . >"$LOG_DIR/${STAMP}-report.json" || true
fi
curl -fsS --max-time 30 -H "X-Hackme-Admin-Token: $ADMIN" \
  "$BASE/api/fuzz/campaigns/${CID_OUT}/escrow" | jq . >"$LOG_DIR/${STAMP}-escrow.json" || true

export CAMPAIGN_ID="$CID_OUT"
bash "$(dirname "$0")/bootstrap_snapshot.sh" "$STAMP" "final-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true

verdict="$(jq -r '.verdict // "?"' "$LOG_DIR/${STAMP}-report.json" 2>/dev/null || echo '?')"
log "DONE target=$TARGET campaign=$CID_OUT runs_done=$runs_done verdict=$verdict"
jq -nc --arg target "$TARGET" --arg cid "$CID_OUT" --arg stamp "$STAMP" \
  --argjson runs "$runs_done" --arg verdict "$verdict" \
  '{target:$target,campaign_id:$cid,stamp:$stamp,runs_done:$runs,verdict:$verdict}' \
  >>"$INSTALL/logs/bootstrap/order_results.jsonl"
