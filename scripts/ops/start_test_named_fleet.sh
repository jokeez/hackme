#!/usr/bin/env bash
# Start N independent Cosmetics/test pool workers (CPU claim/submit, pinned GH/s).
# Durable via systemd --user (survives shell exit). Does NOT touch worker-kapa-pc.
#
# Default: 15 rigs, distinct GH/s from 30 … 60 (evenly spaced; pool display ~500+ GH).
#
#   bash scripts/ops/start_test_named_fleet.sh
#   bash scripts/ops/stop_test_named_fleet.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

N="${N:-15}"
COORD_URL="${COORD_URL:-http://132.243.112.100:18083}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/test-named-fleet}"
SEED_DIR="${SEED_DIR:-$ROOT/logs/test-named-fleet/seeds}"
BATCH_SIZE="${BATCH_SIZE:-2097152}"
GH_MIN="${GH_MIN:-30.0}"
GH_MAX="${GH_MAX:-60.0}"
UNIT_PREFIX="${UNIT_PREFIX:-hackme-test-poh}"

NAMES=(
  desktop-a4m2rx desktop-k7v1pd desktop-q9n4ls desktop-t2c8we desktop-z5h6mf
  shannon turing hopper knuth mccarthy
  lovelace tesla euclid noether faraday
)

TOKEN="${POOL_TOKEN:-}"
if [[ -z "$TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]]; then
  TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token")"
fi
if [[ -z "$TOKEN" ]]; then
  echo "[test-fleet] need POOL_TOKEN or .secrets/hackme_coordinator_worker_token (never admin token)" >&2
  exit 1
fi

MINERSIGN_BIN="${MINERSIGN_BIN:-}"
if [[ -z "$MINERSIGN_BIN" ]]; then
  if [[ -x "$ROOT/minersign" ]]; then MINERSIGN_BIN="$ROOT/minersign"
  elif [[ -x "$ROOT/bin/minersign" ]]; then MINERSIGN_BIN="$ROOT/bin/minersign"
  else echo "[test-fleet] minersign binary missing" >&2; exit 1
  fi
fi

mkdir -p "$LOG_DIR" "$SEED_DIR"
bash "$ROOT/scripts/ops/stop_test_named_fleet.sh" >/dev/null 2>&1 || true
sleep 1

echo "[test-fleet] starting $N workers GH ${GH_MIN}…${GH_MAX} → $COORD_URL (systemd --user)"
for i in $(seq 0 $((N - 1))); do
  name="${NAMES[$i]:-rig$i}"
  wid="worker-${name}"
  unit="${UNIT_PREFIX}-${name}"
  seed_file="$SEED_DIR/${wid}.seed"
  if [[ ! -f "$seed_file" ]]; then
    "$MINERSIGN_BIN" -gen-seed 2>/dev/null | python3 -c 'import sys,json; print(json.load(sys.stdin)["HACKME_MINER_ED25519_SEED_HEX"])' >"$seed_file" \
      || openssl rand -hex 32 >"$seed_file"
  fi
  seed="$(tr -d '\r\n' <"$seed_file")"
  if [[ ${#seed} -ne 64 ]]; then
    echo "[test-fleet] bad seed for $wid (len=${#seed})" >&2
    continue
  fi
  if [[ "$N" -le 1 ]]; then
    gh="$(python3 -c "print(round(float('${GH_MIN}'), 2))")"
  else
    gh="$(python3 -c "print(round(${GH_MIN} + (${GH_MAX}-${GH_MIN})*${i}/(${N}-1), 2))")"
  fi
  logf="$LOG_DIR/${wid}.log"
  : >"$logf"
  # shellcheck disable=SC2086
  systemd-run --user \
    --unit="$unit" \
    --property=Restart=on-failure \
    --property=RestartSec=3 \
    --working-directory="$ROOT" \
    --setenv=COORD_URL="$COORD_URL" \
    --setenv=COORD_ADMIN_TOKEN="$TOKEN" \
    --setenv=WORKER_ID="$wid" \
    --setenv=WORKER_NAME="$name" \
    --setenv=BATCH_SIZE="$BATCH_SIZE" \
    --setenv=HASHRATE_GHS="$gh" \
    --setenv=FORCE_HASHRATE_GHS="$gh" \
    --setenv=HACKME_WORKER_SIGN_SUBMITS=1 \
    --setenv=HACKME_MINER_ED25519_SEED_HEX="$seed" \
    --setenv=HACKME_MINER_NONCE_FILE="$LOG_DIR/${wid}.nonce" \
    --setenv=MINERSIGN_BIN="$MINERSIGN_BIN" \
    --setenv=COORD_PUSH_WORK=1 \
    /bin/bash -c "exec >>\"$logf\" 2>&1; exec bash \"$ROOT/scripts/ops/worker_loop.sh\""
  echo "$unit" >"$LOG_DIR/${wid}.unit"
  echo "[test-fleet]  $wid  gh=${gh}  unit=${unit}  log=$logf"
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
echo "[test-fleet] active $alive / $N"
echo "[test-fleet] stop: bash scripts/ops/stop_test_named_fleet.sh"
