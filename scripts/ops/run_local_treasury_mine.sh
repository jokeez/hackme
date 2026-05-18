#!/usr/bin/env bash
# Local chain + SQLite in HACKME_DATA_DIR: fresh genesis (50k HMC → DevFeeAddress / treasury),
# local WASM PoH on a free port. Does not touch ./data/hackme.db unless HACKME_DATA_DIR is unset.
#
# Uses admin token from .env.desktop if present, else .secrets/hackme_admin_token.
#
#   RUN_SEC=30 bash scripts/ops/run_local_treasury_mine.sh   # demo then stop (default 25)
#   RUN_SEC=0 bash scripts/ops/run_local_treasury_mine.sh    # keep node running (Ctrl+C or kill $PID)
#
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[local-treasury] missing: $1" >&2; exit 1; }; }
require_cmd curl
require_cmd jq
require_cmd go

pick_free_port() {
  python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}

DATA_DIR="${HACKME_DATA_DIR:-$ROOT_DIR/data/local-treasury-mine}"
PORT="${PORT:-$(pick_free_port)}"
BASE="http://127.0.0.1:${PORT}"
RUN_SEC="${RUN_SEC:-25}"

mkdir -p "$DATA_DIR"

ADMIN_TOKEN="${ADMIN_TOKEN:-}"
if [[ -z "$ADMIN_TOKEN" && -f "$ROOT_DIR/.env.desktop" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$ROOT_DIR/.env.desktop" && set +a
  unset HACKME_BEGINNER_SOLO HACKME_ALLOW_LOCAL_SOLO 2>/dev/null || true
  ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"
fi
if [[ -z "$ADMIN_TOKEN" && -f "$ROOT_DIR/.secrets/hackme_admin_token" ]]; then
  ADMIN_TOKEN="$(head -n1 "$ROOT_DIR/.secrets/hackme_admin_token" | tr -d '\r\n')"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[local-treasury] set ADMIN_TOKEN or create .env.desktop / .secrets/hackme_admin_token" >&2
  exit 1
fi

BIN="${BIN:-$ROOT_DIR/hackme-node-local-treasury}"
echo "[local-treasury] go build -> $BIN"
go build -trimpath -o "$BIN" .

export HACKME_DATA_DIR="$DATA_DIR"
export HACKME_BIND_ADDR="127.0.0.1:${PORT}"
export HACKME_CHAIN_LEADER_LOCAL_POH=1
export HACKME_ADMIN_TOKEN="$ADMIN_TOKEN"
export HACKME_REQUIRE_ADMIN_TOKEN=1
unset HACKME_BEGINNER_SOLO HACKME_ALLOW_LOCAL_SOLO HACKME_DESKTOP_MODE 2>/dev/null || true
unset HACKME_P2P_PEERS HACKME_POOL_COORDINATOR_URL HACKME_CANONICAL_CHAIN_URL HACKME_PUBLIC_AUTHORITY_BASE 2>/dev/null || true

LOG="$DATA_DIR/node.log"
echo "[local-treasury] DATA_DIR=$DATA_DIR BASE=$BASE log=$LOG"
"$BIN" >>"$LOG" 2>&1 &
pid=$!

cleanup() {
  kill -TERM "$pid" 2>/dev/null || true
  sleep 0.5
  kill -KILL "$pid" 2>/dev/null || true
}
trap cleanup INT TERM EXIT

for i in $(seq 1 60); do
  if curl -fsS --max-time 2 "$BASE/api/status" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "[local-treasury] node died; tail log:" >&2
    tail -40 "$LOG" >&2
    exit 1
  fi
  sleep 0.4
  if [[ "$i" == "60" ]]; then
    echo "[local-treasury] timeout waiting for API" >&2
    tail -40 "$LOG" >&2
    exit 1
  fi
done

st="$(curl -fsS --max-time 10 "$BASE/api/status")"
if [[ "$(echo "$st" | jq -r '.has_genesis')" != "true" ]]; then
  echo "[local-treasury] posting genesis (50k HMC → treasury DevFeeAddress in economics)"
  curl -fsS --max-time 20 -X POST "$BASE/api/genesis" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d '{}' | jq '{balance, chain_id: .block.chain_id}'
fi

echo "[local-treasury] economics snapshot:"
curl -fsS --max-time 10 "$BASE/api/status" | jq '.economics | {dev_fee_address, total_minted_hmc, circulating_hmc, max_supply_hmc}'

TREASURY="$(curl -fsS --max-time 10 "$BASE/api/status" | jq -r '.economics.dev_fee_address')"
echo "[local-treasury] treasury address: $TREASURY"
curl -fsS --max-time 10 "$BASE/api/address/${TREASURY}" | jq '{address, balance_hmc, balance_units}'

echo "[local-treasury] mining start"
curl -fsS --max-time 15 -X POST "$BASE/api/mining/start" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" | jq .

h0="$(curl -fsS --max-time 10 "$BASE/api/status" | jq -r '.tip_height')"
echo "[local-treasury] tip_height before wait: $h0"
if [[ "${RUN_SEC}" != "0" ]]; then
  sleep "$RUN_SEC"
  h1="$(curl -fsS --max-time 10 "$BASE/api/status" | jq -r '.tip_height')"
  echo "[local-treasury] tip_height after ${RUN_SEC}s: $h1 (mining should advance height)"
  curl -fsS --max-time 10 "$BASE/api/status" | jq '{tip_height, mining, economics: .economics | {total_minted_hmc, circulating_hmc, total_burned_hmc}}'
  curl -fsS --max-time 10 -X POST "$BASE/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null || true
else
  echo "[local-treasury] RUN_SEC=0 — node left running (pid=$pid). Stop: kill $pid"
  trap - INT TERM EXIT
  exit 0
fi

echo "[local-treasury] done OK (db under $DATA_DIR/hackme.db)"
trap - INT TERM EXIT
cleanup
