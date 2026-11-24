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
# Dig package "deep" rejects budget_hmc < 25 on security-audit create.
BUDGET_HMC="${BUDGET_HMC:-25}"
BUDGET_RUNS="${BUDGET_RUNS:-1000}"
MAX_BUDGET_RUNS="${MAX_BUDGET_RUNS:-5000}"
REWARD_HMC="${REWARD_HMC:-0.05}"
TARGET_SOLVES="${TARGET_SOLVES:-8}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="${LOG_DIR:-$INSTALL/logs/bootstrap/orders}"
mkdir -p "$LOG_DIR"
# Hard ceiling so escrow/bot never enqueue 24k-run walls again.
if [[ "$BUDGET_RUNS" -gt "$MAX_BUDGET_RUNS" ]]; then
  echo "[bootstrap-order] clamp BUDGET_RUNS $BUDGET_RUNS -> $MAX_BUDGET_RUNS" >&2
  BUDGET_RUNS="$MAX_BUDGET_RUNS"
fi

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
# shellcheck source=load_coord_token.sh
source "$(dirname "$0")/load_coord_token.sh"
load_bootstrap_coord_token
# Dig detector WASM (property/security) — NOT the PoH order gate.
# Coordinator PoH attach ignores Dig wasm_check_hex and uses the order gate
# (or poh_wasm_check_hex). Shipping the order gate as Dig detector made every
# run look like failed_checks / fake "FINDING" noise (live e2e 2026-10-31).
DIG_WASM="${DIG_WASM_FILE:-${WASM_FILE:-}}"
if [[ -z "$DIG_WASM" ]]; then
  for cand in \
    "$INSTALL/tasks/artifacts/security/rust_overflow_smoke_guard.wasm" \
    "$ROOT/tasks/artifacts/security/rust_overflow_smoke_guard.wasm" \
    "$INSTALL/tasks/artifacts/security/rust_bounds_smoke_guard.wasm" \
    "$ROOT/tasks/artifacts/security/rust_bounds_smoke_guard.wasm"; do
    if [[ -f "$cand" ]]; then DIG_WASM="$cand"; break; fi
  done
fi
if [[ -z "$DIG_WASM" || ! -f "$DIG_WASM" ]]; then
  echo "[bootstrap-order] missing Dig detector wasm (set DIG_WASM_FILE or ship rust_overflow_smoke_guard.wasm)" >&2
  exit 1
fi
WASM_HEX="$(xxd -p "$DIG_WASM" | tr -d '\n')"
[[ -n "$WASM_HEX" ]] || { echo "[bootstrap-order] empty Dig wasm hex" >&2; exit 1; }
log_gate() { echo "[bootstrap-order] Dig detector=$(basename "$DIG_WASM") bytes=$(( ${#WASM_HEX} / 2 ))" >&2; }
log_gate
# Optional: verify order gate artifact exists for coordinator PoH attach (not Dig wasm).
POH_GATE=""
for cand in \
  "$INSTALL/tasks/artifacts/security/upstream_hackme_order_gate.wasm" \
  "$ROOT/tasks/artifacts/security/upstream_hackme_order_gate.wasm" \
  "$ROOT/internal/sandbox/embed/upstream_hackme_order_gate.wasm"; do
  if [[ -f "$cand" ]]; then POH_GATE="$cand"; break; fi
done
if [[ -z "$POH_GATE" && "${HACKME_ALLOW_MINIMAL_POH_GATE:-0}" != "1" ]]; then
  echo "[bootstrap-order] WARN: PoH order gate wasm missing on disk — coordinator attach may fail" >&2
fi
OID="order-bootstrap-${TARGET}-${STAMP}"
CID="campaign-bootstrap-${TARGET}-${STAMP}"
TITLE="HackMe Bootstrap Audit · ${TARGET} · deep pool"

log() { echo "[bootstrap-order $(date -u +%H:%M:%S)] $*" | tee -a "$LOG_DIR/${STAMP}.log"; }

export BOOTSTRAP_INSTALL="$INSTALL" SNAP_DIR="$INSTALL/logs/bootstrap/snapshots"
export CAMPAIGN_ID=""
bash "$(dirname "$0")/bootstrap_snapshot.sh" "$STAMP" "pre-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true

log "POST security-audit target=$TARGET budget_hmc=$BUDGET_HMC runs=$BUDGET_RUNS poh_reward=$REWARD_HMC solves=$TARGET_SOLVES"
http_code="$(curl -sS --max-time 120 -o "$LOG_DIR/${STAMP}-audit.raw.json" -w '%{http_code}' -X POST "$BASE/api/security-audit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$(jq -nc \
    --arg title "$TITLE" \
    --arg payer "${PAYER_REF:-HMC-23336d92da342724}" \
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
      budget_seconds: 28800,
      depth_tier: "bytes_corpus",
      guard_name: ("upstream_" + $target),
      create_poh_order: true,
      reward_hmc: $reward,
      target_solves: $solves,
      difficulty_score: 10,
      pool_distributed: true,
      use_sup_discount: false
    }')")"
resp="$(cat "$LOG_DIR/${STAMP}-audit.raw.json")"
echo "$resp" | jq . >"$LOG_DIR/${STAMP}-audit.json" 2>/dev/null || cp "$LOG_DIR/${STAMP}-audit.raw.json" "$LOG_DIR/${STAMP}-audit.json"
if [[ "$http_code" != "200" ]]; then
  log "FAIL create HTTP $http_code: $(jq -c . "$LOG_DIR/${STAMP}-audit.json" 2>/dev/null || cat "$LOG_DIR/${STAMP}-audit.raw.json")"
  exit 1
fi
TOK="$(jq -r '.customer_report_token // empty' <<<"$resp")"
CID_OUT="$(jq -r '.campaign_id // empty' <<<"$resp")"
[[ -n "$CID_OUT" ]] || { log "FAIL create: $(cat "$LOG_DIR/${STAMP}-audit.json")"; exit 1; }
log "created campaign=$CID_OUT order=$(jq -c '.order // {}' <<<"$resp") pool_sync=$(jq -r '.pool_sync // ""' <<<"$resp")"

POOL_SYNC="$(jq -r '.pool_sync // ""' <<<"$resp")"
case "$POOL_SYNC" in
  ok|sync|synced) log "pool_sync already $POOL_SYNC — skip resync retries" ;;
  queued|"")
    log "pool_sync ${POOL_SYNC:-empty} — pushing to coordinator (bounded retries)"
    sync_ok=0
    for attempt in 1 2 3; do
      if CAMPAIGN_ID="$CID_OUT" RESYNC_CURL_MAX_TIME=45 \
        bash "$(dirname "$0")/bootstrap_resync_pool.sh" >>"$LOG_DIR/${STAMP}.log" 2>&1; then
        log "pool_sync ok attempt=$attempt"
        sync_ok=1
        break
      fi
      log "pool_sync retry attempt=$attempt failed; sleep ${attempt}s"
      sleep "$attempt"
    done
    if [[ "$sync_ok" -ne 1 ]]; then
      log "WARN pool_sync still pending after retries — continuing (CREATE already succeeded)"
    fi
    ;;
  *)
    log "WARN unexpected pool_sync=$POOL_SYNC — continuing (CREATE already succeeded)"
    ;;
esac

export CAMPAIGN_ID="$CID_OUT"
bash "$(dirname "$0")/bootstrap_snapshot.sh" "$STAMP" "post-create-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true

# PLACE_ONLY=1: exit after create/resync (Dig bot / timers must not hang for hours).
if [[ "${PLACE_ONLY:-0}" == "1" ]]; then
  log "PLACE_ONLY=1 — done after create campaign=$CID_OUT"
  jq -nc --arg target "$TARGET" --arg cid "$CID_OUT" --arg stamp "$STAMP" --arg oid "$OID" \
    --argjson runs 0 --argjson poh 0 --arg verdict "placed" \
    '{target:$target,campaign_id:$cid,order_id:$oid,stamp:$stamp,runs_done:$runs,poh_progress:$poh,verdict:$verdict}' \
    >>"$INSTALL/logs/bootstrap/order_results.jsonl" || true
  exit 0
fi

POLL_SEC="${POLL_SEC:-120}"
MAX_WAIT="${MAX_WAIT:-7200}"
ORDERS_PUBLIC="${ORDERS_PUBLIC:-https://hackme.tech}"
deadline=$(( $(date +%s) + MAX_WAIT ))
runs_done=0
poh_progress=0
if [[ -z "$COORD_POLL_TOKEN" ]]; then
  log "WARN COORD_POLL_TOKEN empty — progress will look like runs_done=0 (unauthorized)"
fi
while [[ $(date +%s) -lt $deadline ]]; do
  sleep "$POLL_SEC"
  prog="$(curl -fsS --max-time 30 -H "X-Hackme-Admin-Token: ${COORD_POLL_TOKEN}" "$COORD/api/fuzz/pool/campaigns/progress?id=${CID_OUT}" 2>/dev/null || echo '{}')"
  runs_done="$(jq -r '.runs_done // 0' <<<"$prog")"
  status="$(jq -r '.status // ""' <<<"$prog")"
  # Kick local node sync (pulse) so report/escrow see the same runs_done as the pool.
  if [[ -n "$TOK" ]]; then
    curl -fsS --max-time 20 "$BASE/api/fuzz/campaigns/${CID_OUT}/pulse" \
      -H "X-Hackme-Report-Token: $TOK" >/dev/null 2>&1 || true
  fi
  work="$(curl -fsS --max-time 15 "$COORD/api/work/stats" 2>/dev/null || echo '{}')"
  # PoH progress lives on the command chain /api/tasks (not fuzz runs_done).
  poh_progress="$(curl -fsS --max-time 30 "$ORDERS_PUBLIC/api/tasks" 2>/dev/null \
    | jq -r --arg oid "$OID" '.tasks[]? | select(.id==$oid) | .progress_count // 0' 2>/dev/null | head -1)"
  poh_progress="${poh_progress:-0}"
  log "progress runs_done=$runs_done poh=$poh_progress/$TARGET_SOLVES status=$status scheduler=$(jq -r '.scheduler_mode // "?"' <<<"$work") orders_active=$(jq -r '.orders_active // false' <<<"$work")"
  export CAMPAIGN_ID="$CID_OUT"
  bash "$(dirname "$0")/bootstrap_snapshot.sh" "${STAMP}-p$(date +%s)" "poll-${TARGET}" >>"$LOG_DIR/${STAMP}.log" 2>&1 || true
  if [[ "$status" == "completed" || "$status" == "cancelled" ]]; then
    break
  fi
  if [[ "${runs_done:-0}" -ge "$BUDGET_RUNS" ]]; then
    break
  fi
  if [[ "${poh_progress:-0}" -ge "$TARGET_SOLVES" ]]; then
    log "PoH order complete progress=$poh_progress/$TARGET_SOLVES"
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
log "DONE target=$TARGET campaign=$CID_OUT runs_done=$runs_done poh_progress=$poh_progress/$TARGET_SOLVES verdict=$verdict"
jq -nc --arg target "$TARGET" --arg cid "$CID_OUT" --arg stamp "$STAMP" --arg oid "$OID" \
  --argjson runs "$runs_done" --argjson poh "${poh_progress:-0}" --arg verdict "$verdict" \
  '{target:$target,campaign_id:$cid,order_id:$oid,stamp:$stamp,runs_done:$runs,poh_progress:$poh,verdict:$verdict}' \
  >>"$INSTALL/logs/bootstrap/order_results.jsonl"
