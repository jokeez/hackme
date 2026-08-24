#!/usr/bin/env bash
# DEPRECATED for display fleets: prefer start_test_named_fleet.sh (PoH+fuzz same worker_id).
# This script starts separate *-fuzz diggers (sybil rows). Use only for load tests.
# Does NOT touch worker-kapa-pc.
#
#   bash scripts/ops/start_test_named_fuzz_fleet.sh
#   bash scripts/ops/stop_test_named_fuzz_fleet.sh
echo "[fuzz-fleet] WARN: prefer hybrid named fleet (scripts/ops/start_test_named_fleet.sh)" >&2
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

N="${N:-15}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/test-named-fuzz-fleet}"
SEED_DIR="${SEED_DIR:-$ROOT/logs/test-named-fuzz-fleet/seeds}"
TIMEOUT_MS="${WORKERFUZZ_TIMEOUT_MS:-2000}"
HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-90}"
UNIT_PREFIX="${UNIT_PREFIX:-hackme-test-fuzz}"
BIN="${WORKERFUZZ_BIN:-}"
if [[ -z "$BIN" ]]; then
  if [[ -x "$ROOT/bin/workerfuzz" ]]; then BIN="$ROOT/bin/workerfuzz"
  elif [[ -x "$ROOT/workerfuzz" ]]; then BIN="$ROOT/workerfuzz"
  else echo "[fuzz-fleet] workerfuzz binary missing" >&2; exit 1
  fi
fi

NAMES=(
  ashwood blackout coldline digsite eastwind
  faraday graphite harbour ironclad jackknife
  keystone lantern mercury northstar overdrive
  redline skyhook timber vault waypoint
)

TOKEN="${POOL_TOKEN:-${COORD_TOKEN:-}}"
if [[ -z "$TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]]; then
  TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token")"
fi
if [[ -z "$TOKEN" ]]; then
  echo "[fuzz-fleet] need POOL_TOKEN / COORD_TOKEN" >&2
  exit 1
fi

MINERSIGN_BIN="${MINERSIGN_BIN:-}"
if [[ -z "$MINERSIGN_BIN" ]]; then
  if [[ -x "$ROOT/minersign" ]]; then MINERSIGN_BIN="$ROOT/minersign"
  elif [[ -x "$ROOT/bin/minersign" ]]; then MINERSIGN_BIN="$ROOT/bin/minersign"
  fi
fi

mkdir -p "$LOG_DIR" "$SEED_DIR" "$LOG_DIR/locks"
bash "$ROOT/scripts/ops/stop_test_named_fuzz_fleet.sh" >/dev/null 2>&1 || true
sleep 1

echo "[fuzz-fleet] starting $N workerfuzz → $COORD_URL (systemd --user)"
for i in $(seq 0 $((N - 1))); do
  name="${NAMES[$i]:-rig$i}"
  wid="worker-${name}-fuzz"
  unit="${UNIT_PREFIX}-${name}"
  seed_file="$SEED_DIR/${wid}.seed"
  if [[ ! -f "$seed_file" ]]; then
    if [[ -n "$MINERSIGN_BIN" ]]; then
      "$MINERSIGN_BIN" -gen-seed 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["HACKME_MINER_ED25519_SEED_HEX"])' >"$seed_file" \
        || openssl rand -hex 32 >"$seed_file"
    else
      openssl rand -hex 32 >"$seed_file"
    fi
  fi
  seed="$(tr -d '\r\n' <"$seed_file")"
  logf="$LOG_DIR/${wid}.log"
  : >"$logf"
  systemd-run --user \
    --unit="$unit" \
    --property=Restart=always \
    --property=RestartSec=3 \
    --working-directory="$ROOT" \
    --setenv=COORD_URL="$COORD_URL" \
    --setenv=COORD_TOKEN="$TOKEN" \
    --setenv=WORKER_ID="$wid" \
    --setenv=HACKME_WORKER_SIGN_SUBMITS=1 \
    --setenv=HACKME_MINER_ED25519_SEED_HEX="$seed" \
    --setenv=HACKME_WORKER_LOCK_DIR="$LOG_DIR/locks" \
    --setenv=WORKERFUZZ_HTTP_TIMEOUT_SEC="$HTTP_TIMEOUT_SEC" \
    /bin/bash -c "exec >>\"$logf\" 2>&1; exec \"$BIN\" -coord \"$COORD_URL\" -token \"$TOKEN\" -worker \"$wid\" -timeout-ms \"$TIMEOUT_MS\""
  echo "$unit" >"$LOG_DIR/${wid}.unit"
  echo "[fuzz-fleet]  $wid  unit=${unit}  log=$logf"
done

sleep 3
alive=0
for i in $(seq 0 $((N - 1))); do
  name="${NAMES[$i]:-rig$i}"
  unit="${UNIT_PREFIX}-${name}"
  if systemctl --user is-active --quiet "$unit.service" 2>/dev/null; then
    alive=$((alive + 1))
  fi
done
echo "[fuzz-fleet] active $alive / $N"
echo "[fuzz-fleet] stop: bash scripts/ops/stop_test_named_fuzz_fleet.sh"
