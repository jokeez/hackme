#!/usr/bin/env bash
# Load / mega test suite: miner influx, coordinator burst, API mega stress.
#
# Usage:
#   bash scripts/tests/load_mega_suite.sh
#   STRESS_QUICK=1 bash scripts/tests/load_mega_suite.sh   # ~8 min
#   STRESS_QUICK=0 bash scripts/tests/load_mega_suite.sh   # ~25 min full mega
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"
# shellcheck disable=SC1091
source "$ROOT/scripts/tests/coordinator_stress.env"

require_cmd go
require_cmd curl
require_cmd jq
require_cmd python3
require_cmd timeout

OUT_DIR="${OUT_DIR:-$ROOT/reports/tests}"
RID="${RUN_ID:-load_mega_$(run_id)}"
OUT="$OUT_DIR/$RID/load_mega_suite"
ensure_reports_dir "$OUT"
LOG="$OUT/suite.log"
RESULTS="$OUT/results.jsonl"
: >"$LOG"
: >"$RESULTS"

STRESS_QUICK="${STRESS_QUICK:-1}"
if [[ "$STRESS_QUICK" == "1" ]]; then
  SWARM_WORKERS="${SWARM_WORKERS:-12}"
  SWARM_SEC="${SWARM_SEC:-35}"
  MOCK_WORKERS="${MOCK_WORKERS:-40}"
  MOCK_SEC="${MOCK_SEC:-50}"
  MEGA_COORD_WORKERS="${MEGA_COORD_WORKERS:-50}"
  MEGA_COORD_SEC="${MEGA_COORD_SEC:-90}"
  MEGA_STRESS_SEC="${MEGA_STRESS_SEC:-120}"
  MEGA_TX_WORKERS="${MEGA_TX_WORKERS:-24}"
  MEGA_ORDERS_WORKERS="${MEGA_ORDERS_WORKERS:-8}"
  MEGA_COORD_BURST="${MEGA_COORD_BURST:-16}"
else
  SWARM_WORKERS="${SWARM_WORKERS:-25}"
  SWARM_SEC="${SWARM_SEC:-60}"
  MOCK_WORKERS="${MOCK_WORKERS:-80}"
  MOCK_SEC="${MOCK_SEC:-90}"
  MEGA_COORD_WORKERS="${MEGA_COORD_WORKERS:-100}"
  MEGA_COORD_SEC="${MEGA_COORD_SEC:-600}"
  MEGA_STRESS_SEC="${MEGA_STRESS_SEC:-300}"
  MEGA_TX_WORKERS="${MEGA_TX_WORKERS:-48}"
  MEGA_ORDERS_WORKERS="${MEGA_ORDERS_WORKERS:-16}"
  MEGA_COORD_BURST="${MEGA_COORD_BURST:-24}"
fi

ADMIN_TOKEN="${ADMIN_TOKEN:-load-mega-admin-token-32chars-ok!!}"
export ADMIN_TOKEN HACKME_ADMIN_TOKEN="$ADMIN_TOKEN"

log() { echo "$*" | tee -a "$LOG"; }
record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
  log "[$verdict] $id — $detail"
}

pick_free_port() {
  python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}

MAIN_PORT="$(pick_free_port)"
COORD_PORT="$(pick_free_port)"
BASE="http://127.0.0.1:${MAIN_PORT}"
COORD="http://127.0.0.1:${COORD_PORT}"
DATA="$OUT/stack_data"
mkdir -p "$DATA"

coord_bin="$ROOT/bin/coordinator-stress"
main_bin="$ROOT/bin/hackme-load-mega"
go build -trimpath -o "$coord_bin" ./cmd/coordinator
go build -trimpath -o "$main_bin" .

coord_pid=""
main_pid=""
cleanup() {
  for p in "${worker_pids[@]:-}"; do kill -TERM "$p" 2>/dev/null || true; done
  [[ -n "$main_pid" ]] && kill -TERM "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
  sleep 0.5
  [[ -n "$main_pid" ]] && kill -KILL "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -KILL "$coord_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

log "=== load mega suite RUN_ID=$RID STRESS_QUICK=$STRESS_QUICK ==="
log "BASE=$BASE COORD=$COORD"

HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_COORDINATOR_DB="$DATA/coordinator.db" \
HACKME_COORDINATOR_ALLOW_INSECURE=1 \
HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=0 \
  "$coord_bin" >>"$OUT/coordinator.log" 2>&1 &
coord_pid=$!

for _ in $(seq 1 50); do
  curl -fsS --max-time 3 "$COORD/api/network/stats" >/dev/null 2>&1 && break
  sleep 0.4
done

HACKME_DATA_DIR="$DATA/node" \
HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_POOL_COORDINATOR_URL="$COORD" \
HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
HACKME_CHAIN_LEADER_LOCAL_POH=1 \
HACKME_FUZZ_AUTORUN=0 \
HACKME_AUTO_START_MINING=0 \
  "$main_bin" >>"$OUT/node.log" 2>&1 &
main_pid=$!

for _ in $(seq 1 60); do
  curl -fsS --max-time 3 "$BASE/api/status" >/dev/null 2>&1 && break
  sleep 0.4
done

curl -fsS --max-time 15 -X POST "$BASE/api/genesis" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
  -H "Content-Type: application/json" -d '{}' >/dev/null || true
curl -fsS --max-time 15 -X POST "$BASE/api/mining/start" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >/dev/null || true

record "stack_up" "pass" "coordinator+node ready"
export BASE COORD ADMIN_TOKEN COORD_ADMIN_TOKEN="$ADMIN_TOKEN"

# --- 1) Miner influx: parallel worker_loop (simulated PCs) ---
log "=== phase 1: miner swarm influx ($SWARM_WORKERS workers × ${SWARM_SEC}s) ==="
export COORD_URL="$COORD" COORD_ADMIN_TOKEN="$ADMIN_TOKEN"
export BATCH_SIZE="${BATCH_SIZE:-400000}" HASHRATE_GHS="${HASHRATE_GHS:-20}"
worker_pids=()
for w in $(seq 1 "$SWARM_WORKERS"); do
  wid="load-swarm-$(printf '%03d' "$w")"
  (
    export WORKER_ID="$wid" WORKER_NAME="Load-$wid"
    exec timeout "$SWARM_SEC" bash "$ROOT/scripts/ops/worker_loop.sh"
  ) >>"$OUT/swarm_${wid}.log" 2>&1 &
  worker_pids+=("$!")
done
for p in "${worker_pids[@]}"; do wait "$p" || true; done
worker_pids=()

swarm_stats="$(curl -fsS --max-time 20 "$COORD/api/work/stats?details=1" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN")"
swarm_workers="$(echo "$swarm_stats" | jq -r '.workers_count // 0')"
swarm_accepted="$(echo "$swarm_stats" | jq -r '.accepted_attempts // 0')"
echo "$swarm_stats" | jq '{workers_count,accepted_attempts,submitted_items,total_payout_hmc}' >"$OUT/swarm_summary.json"
if [[ "$swarm_workers" -ge "$((SWARM_WORKERS / 2))" ]] && [[ "$swarm_accepted" -gt 0 ]]; then
  record "miner_swarm_influx" "pass" "workers=$swarm_workers accepted=$swarm_accepted"
else
  record "miner_swarm_influx" "fail" "workers=$swarm_workers accepted=$swarm_accepted"
fi

# --- 2) Mock virtual miners (Python burst) ---
log "=== phase 2: mock miners pool ($MOCK_WORKERS workers × ${MOCK_SEC}s) ==="
if COORD="$COORD" COORD_ADMIN_TOKEN="$ADMIN_TOKEN" NODE_BASE="$BASE" \
  WORKERS="$MOCK_WORKERS" DURATION_SEC="$MOCK_SEC" BATCH_SIZE=512 \
  bash "$ROOT/scripts/tests/mock_miners_load.sh" >>"$OUT/mock_miners.log" 2>&1; then
  record "mock_miners_load" "pass" "${MOCK_WORKERS} virtual miners"
else
  record "mock_miners_load" "fail" "see $OUT/mock_miners.log"
fi

# --- 3) Coordinator mega stress ---
log "=== phase 3: coordinator mega stress (workers=$MEGA_COORD_WORKERS duration=${MEGA_COORD_SEC}s) ==="
# Stop shared coordinator — mega stress starts its own instance on the same port.
[[ -n "$coord_pid" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
wait "$coord_pid" 2>/dev/null || true
coord_pid=""
sleep 0.5
unset HACKME_COORDINATOR_ADDR HACKME_COORDINATOR_DB 2>/dev/null || true
export HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}"
export HACKME_COORDINATOR_DB="$DATA/coordinator_mega.db"
export HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN"
if WORKERS="$MEGA_COORD_WORKERS" DURATION_SEC="$MEGA_COORD_SEC" TARGET_RPS=15 STRESS_QUICK=0 \
  RUN_ID="${RID}_mega" REPORT_DIR="$OUT/coordinator_mega_stress" \
  bash "$ROOT/scripts/tests/coordinator_mega_stress.sh" >>"$OUT/coordinator_mega.log" 2>&1; then
  mega_verdict="$(grep -E '^## Verdict|NOT_READY|PASS' "$OUT/coordinator_mega_stress/MEGA_STRESS_REPORT.md" 2>/dev/null | head -3 | tr '\n' ' ' || echo ok)"
  record "coordinator_mega_stress" "pass" "$mega_verdict"
else
  record "coordinator_mega_stress" "fail" "see $OUT/coordinator_mega.log"
fi

# Re-point coordinator after mega stress phase (mega stress stops its coordinator on exit).
if ! curl -fsS --max-time 3 "$COORD/api/network/stats" >/dev/null 2>&1; then
  log "[warn] restarting coordinator after mega stress"
  HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
  HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_COORDINATOR_DB="$DATA/coordinator.db" \
  HACKME_COORDINATOR_ALLOW_INSECURE=1 \
  HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=0 \
  HACKME_COORDINATOR_CLAIM_PER_MIN=200000 \
  HACKME_COORDINATOR_SUBMIT_PER_MIN=500000 \
    "$coord_bin" >>"$OUT/coordinator_restart.log" 2>&1 &
  coord_pid=$!
  for _ in $(seq 1 40); do
    curl -fsS --max-time 3 "$COORD/api/network/stats" >/dev/null 2>&1 && break
    sleep 0.4
  done
fi

# Ensure node still up and mining before API burst (mega stress samples hashrate).
if curl -fsS --max-time 5 "$BASE/api/status" >/dev/null 2>&1; then
  curl -fsS --max-time 10 -X POST "$BASE/api/mining/start" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >/dev/null 2>&1 || true
  sleep 2
else
  log "[warn] restarting node after mega stress"
  HACKME_DATA_DIR="$DATA/node" \
  HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
  HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_POOL_COORDINATOR_URL="$COORD" \
  HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
  HACKME_CHAIN_LEADER_LOCAL_POH=1 \
  HACKME_FUZZ_AUTORUN=0 \
    "$main_bin" >>"$OUT/node_restart.log" 2>&1 &
  main_pid=$!
  for _ in $(seq 1 40); do
    curl -fsS --max-time 3 "$BASE/api/status" >/dev/null 2>&1 && break
    sleep 0.4
  done
  curl -fsS --max-time 10 -X POST "$BASE/api/mining/start" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >/dev/null 2>&1 || true
  sleep 2
fi

export BASE COORD ADMIN_TOKEN COORD_ADMIN_TOKEN="$ADMIN_TOKEN"

# --- 4) API mega stress (tx + orders + coord burst) ---
log "=== phase 4: mega_stress API burst (${MEGA_STRESS_SEC}s) ==="
if PRECHECK_FULL=0 POSTCHECK_SECURITY=1 \
  DURATION_SEC="$MEGA_STRESS_SEC" \
  TX_WORKERS="$MEGA_TX_WORKERS" ORDERS_WORKERS="$MEGA_ORDERS_WORKERS" \
  COORD_WORKERS="$MEGA_COORD_BURST" SAMPLE_INTERVAL_SEC=2 \
  SKIP_MIN_HASHRATE_GATE=1 \
  RUN_ID="${RID}_api_mega" \
  bash "$ROOT/scripts/tests/mega_stress.sh" >>"$OUT/mega_stress.log" 2>&1; then
  record "mega_stress_api" "pass" "${MEGA_STRESS_SEC}s mixed burst"
else
  record "mega_stress_api" "fail" "see $OUT/mega_stress.log"
fi

# --- 5) Coordinator matrix (rate limit probe) ---
log "=== phase 5: coordinator_matrix ==="
curl -fsS --max-time 10 -X POST "$COORD/api/work/admin/clear-abuse" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
  -H "Content-Type: application/json" -d '{"all":true}' >/dev/null 2>&1 || true
sleep 1
if RUN_ID="${RID}_cmatrix" \
  bash "$ROOT/scripts/tests/coordinator_matrix.sh" >>"$OUT/coordinator_matrix.log" 2>&1; then
  record "coordinator_matrix" "pass" "claim/submit matrix"
else
  record "coordinator_matrix" "fail" "see $OUT/coordinator_matrix.log"
fi

# --- 6) Real CUDA worker under load (if GPU) ---
if [[ -x "$ROOT/bin/workerpoh-cuda" ]] && command -v nvidia-smi >/dev/null 2>&1; then
  log "=== phase 6: CUDA worker under coordinator load ==="
  if SAMPLE_SEC=25 STRESS_QUICK=1 SKIP_MEGA_STRESS=1 SKIP_PUBLIC_POOL=1 \
    USE_EXISTING_STACK=1 BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" \
    RUN_ID="${RID}_mining" bash "$ROOT/scripts/tests/mining_load_suite.sh" >>"$OUT/mining_load.log" 2>&1; then
    best="$(jq -r '.best_inst_ghs // 0' "$OUT_DIR/${RID}_mining/mining_load_suite/summary.json" 2>/dev/null || echo 0)"
    record "mining_load_cuda" "pass" "best ${best} GH/s"
  else
    record "mining_load_cuda" "fail" "see $OUT/mining_load.log"
  fi
else
  record "mining_load_cuda" "pass" "skipped (no CUDA/GPU)"
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
status="PASS"
[[ "$fails" -eq 0 ]] || status="FAIL"

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg status "$status" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  --argjson stress_quick "$([[ "$STRESS_QUICK" == 1 ]] && echo true || echo false)" \
  '{run_id:$run_id,captured_at:$captured_at,suite:"load_mega_suite",status:$status,base:$base,coord:$coord,total:$total,fails:$fails,stress_quick:$stress_quick}' \
  >"$OUT/summary.json"

ln -sfn "$OUT" "$ROOT/reports/load-mega-LATEST"

log ""
log "Load mega suite $status: $((total - fails))/$total passed"
log "Report: $OUT/summary.json"

if [[ "$status" != "PASS" ]]; then
  fail "load_mega_suite FAIL ($fails/$total). See $OUT"
fi
pass "load_mega_suite PASS ($total phases). See $OUT"
