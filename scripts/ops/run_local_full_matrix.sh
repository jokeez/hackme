#!/usr/bin/env bash
# Ephemeral coordinator + node with auto-mining, then fuzz release gate + MODE=full daily pack.
# Usage (repo root): bash scripts/ops/run_local_full_matrix.sh
# Env:
#   ADMIN_TOKEN — optional; ephemeral hex generated if unset
#   SKIP_PHASE_A=1 — skip go vet / go test (manifest+WASM ABI run once in Phase C via run_daily language-static-pack)
#   SKIP_GO_TESTS=1 — skip go test only (when Phase A runs)
#   MINING_WAIT_SEC — max seconds polling wallet for funded orders case (default 120)
#   RUN_ID — defaults to local-full-UTC timestamp

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

SKIP_PHASE_A="${SKIP_PHASE_A:-0}"
SKIP_GO_TESTS="${SKIP_GO_TESTS:-0}"
MINING_WAIT_SEC="${MINING_WAIT_SEC:-120}"
MIN_WALLET_FOR_ORDERS="${MIN_WALLET_FOR_ORDERS:-0.02}"
RUN_ID="${RUN_ID:-local-full-$(date -u +"%Y%m%dT%H%M%SZ")}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
export RUN_ID

pick_free_port() {
  python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[local-full-matrix] missing: $1" >&2; exit 1; }; }
require_cmd go
require_cmd curl
require_cmd jq
require_cmd python3

if [[ -z "$ADMIN_TOKEN" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    ADMIN_TOKEN="$(openssl rand -hex 24)"
  else
    ADMIN_TOKEN="local-full-matrix-$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
  fi
  echo "[local-full-matrix] ephemeral ADMIN_TOKEN (not persisted)"
fi
export ADMIN_TOKEN
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN_TOKEN}"

phase_a() {
  [[ "$SKIP_PHASE_A" == "1" ]] && return 0
  echo "== Phase A: go vet + go test (task manifest/WASM static → Phase C language-static-pack) =="
  go vet ./...
  if [[ "$SKIP_GO_TESTS" != "1" ]]; then
    go test ./... -count=1
  else
    echo "[local-full-matrix] SKIP_GO_TESTS=1 — skipping go test"
  fi
}

phase_a

MAIN_PORT="${MAIN_PORT:-$(pick_free_port)}"
COORD_PORT="${COORD_PORT:-$(pick_free_port)}"
MAIN_BASE="http://127.0.0.1:${MAIN_PORT}"
COORD_BASE="http://127.0.0.1:${COORD_PORT}"
WORKDIR="${WORKDIR:-/tmp/hackme-local-full-matrix-$$}"

kill_port_listeners() {
  local port="$1"
  if command -v fuser >/dev/null 2>&1; then
    fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
    sleep 0.3
    fuser -k -KILL "${port}/tcp" >/dev/null 2>&1 || true
  fi
}

echo "[local-full-matrix] workdir $WORKDIR ports main=$MAIN_PORT coord=$COORD_PORT"
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR/data"
rsync -a \
  --exclude '.git/' --exclude 'data' --exclude 'data/' --exclude 'reports/' \
  --exclude 'node_modules/' --exclude 'dist/' --exclude 'backups/' --exclude 'logs/' \
  --exclude '.env' --exclude '.env.*' \
  "$ROOT_DIR/" "$WORKDIR/"
cd "$WORKDIR"

mkdir -p "$WORKDIR/bin"
coord_bin="$WORKDIR/bin/coordinator"
main_bin="$WORKDIR/bin/hackme-node"
echo "[local-full-matrix] go build -> $WORKDIR/bin/"
go build -o "$coord_bin" ./cmd/coordinator
go build -o "$main_bin" .

coord_log="$WORKDIR/coordinator.log"
main_log="$WORKDIR/main.log"
coord_pid=""
main_pid=""

cleanup() {
  local ec=$?
  [[ -n "$main_pid" ]] && kill -TERM "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
  sleep 0.5
  [[ -n "$main_pid" ]] && kill -KILL "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -KILL "$coord_pid" 2>/dev/null || true
  kill_port_listeners "$MAIN_PORT"
  kill_port_listeners "$COORD_PORT"
  wait 2>/dev/null || true
  cd "$ROOT_DIR"
  exit "$ec"
}
trap cleanup INT TERM EXIT

echo "[local-full-matrix] starting coordinator $COORD_BASE"
HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_COORDINATOR_DB="$WORKDIR/data/coordinator.db" \
  "$coord_bin" >>"$coord_log" 2>&1 &
coord_pid=$!

for i in $(seq 1 40); do
  if curl -fsS --max-time 3 "$COORD_BASE/api/network/stats" >/dev/null 2>&1; then
    echo "[local-full-matrix] coordinator up"
    break
  fi
  if ! kill -0 "$coord_pid" 2>/dev/null; then
    echo "[local-full-matrix] coordinator died; log:" >&2
    tail -40 "$coord_log" >&2
    exit 1
  fi
  sleep 0.5
  if (( i == 40 )); then
    echo "[local-full-matrix] coordinator timeout" >&2
    tail -40 "$coord_log" >&2
    exit 1
  fi
done

echo "[local-full-matrix] starting node $MAIN_BASE (auto-mining + pool coordinator)"
HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
HACKME_POOL_COORDINATOR_URL="$COORD_BASE" \
HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
HACKME_FUZZ_AUTORUN=0 \
HACKME_AUTO_START_MINING=1 \
HACKME_CHAIN_LEADER_LOCAL_POH=1 \
  "$main_bin" >>"$main_log" 2>&1 &
main_pid=$!

for i in $(seq 1 60); do
  if curl -fsS --max-time 5 "$MAIN_BASE/api/status" >/dev/null 2>&1; then
    echo "[local-full-matrix] node up"
    break
  fi
  if ! kill -0 "$main_pid" 2>/dev/null; then
    echo "[local-full-matrix] node died; log:" >&2
    tail -80 "$main_log" >&2
    exit 1
  fi
  sleep 0.5
  if (( i == 60 )); then
    echo "[local-full-matrix] node timeout" >&2
    tail -80 "$main_log" >&2
    exit 1
  fi
done

st="$(curl -fsS --max-time 10 "$MAIN_BASE/api/status")"
if [[ "$(echo "$st" | jq -r '.has_genesis')" != "true" ]]; then
  echo "[local-full-matrix] posting genesis"
  curl -fsS --max-time 15 -X POST "$MAIN_BASE/api/genesis" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{}' >/dev/null
fi

# HACKME_AUTO_START_MINING only probes genesis for ~5s at startup; genesis is posted later, so start miner explicitly.
echo "[local-full-matrix] ensuring local PoH mining (solo allowed)"
curl -sS --max-time 15 -X POST "$MAIN_BASE/api/mining/start" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" -o /dev/null || true

echo "[local-full-matrix] status/metrics snapshot"
curl -fsS --max-time 10 "$MAIN_BASE/api/status" | jq '{tip_height, mining, has_genesis, network_mode_active, local_solo_allowed}'
curl -fsS --max-time 12 "$MAIN_BASE/api/status" | jq '{pool_target_mod, pool_target_mod_source, pool_global_hashrate_th_s, pool_workers_count}'
curl -fsS --max-time 10 "$MAIN_BASE/api/metrics" | jq '{mining_target_mod, mining_target_block_sec}' || true
curl -fsS --max-time 8 "$COORD_BASE/api/work/stats?details=0" | jq '{target_mod, workers_count}' || true

echo "[local-full-matrix] waiting up to ${MINING_WAIT_SEC}s for wallet >= ${MIN_WALLET_FOR_ORDERS} HMC (orders_matrix positive case)"
for ((w = 0; w < MINING_WAIT_SEC; w += 4)); do
  bal="$(curl -fsS --max-time 8 "$MAIN_BASE/api/wallet" | jq -r '.balance_hmc // 0')" || bal="0"
  if python3 - "$bal" "$MIN_WALLET_FOR_ORDERS" <<'PY'
import sys
b, m = float(sys.argv[1]), float(sys.argv[2])
raise SystemExit(0 if b >= m - 1e-12 else 1)
PY
  then
    echo "[local-full-matrix] wallet balance_hmc=$bal"
    break
  fi
  sleep 4
done

cd "$ROOT_DIR"
export BASE="$MAIN_BASE"
export COORD="$COORD_BASE"
export COORD_ADMIN_TOKEN="$ADMIN_TOKEN"

echo "== Phase B: fuzz_release_gate =="
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" bash scripts/ops/fuzz_release_gate.sh

echo "== Phase C: run_daily MODE=full =="
# Ephemeral local PoH often runs faster than mining_target_block_sec until retarget settles; avoid false FAIL on ratio.
export MAX_FAST_RATIO="${MAX_FAST_RATIO:-0.001}"
MODE=full RUN_ID="${RUN_ID}_daily" BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" \
  bash scripts/tests/run_daily.sh

echo "[local-full-matrix] PASS — RUN_ID=$RUN_ID reports under $ROOT_DIR/reports/tests/"
trap - INT TERM EXIT
kill -TERM "$main_pid" 2>/dev/null || true
kill -TERM "$coord_pid" 2>/dev/null || true
sleep 0.5
kill -KILL "$main_pid" 2>/dev/null || true
kill -KILL "$coord_pid" 2>/dev/null || true
kill_port_listeners "$MAIN_PORT"
kill_port_listeners "$COORD_PORT"
wait 2>/dev/null || true
