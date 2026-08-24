#!/usr/bin/env bash
# Start N named hybrid pool workers: cosmetic PoH GH + fuzz dig under the SAME worker_id.
# Durable via systemd --user. Does NOT touch worker-kapa-pc.
# Prefer this over start_test_named_fuzz_fleet.sh (no separate *-fuzz sybil rows).
#
#   bash scripts/ops/start_test_named_fleet.sh
#   HACKME_NAMED_HYBRID_FUZZ=0 bash scripts/ops/start_test_named_fleet.sh   # PoH-only cosmetics
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
# Hybrid fuzz under same worker_id (default ON). Soft defaults to limit SQLITE_BUSY on hub.
HYBRID_FUZZ="${HACKME_NAMED_HYBRID_FUZZ:-1}"
HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-90}"
FUZZ_GAP_MS="${HACKME_WORKER_HYBRID_FUZZ_CLAIM_GAP_MS:-400}"
FUZZ_TIMEOUT_MS="${NAMED_FUZZ_TIMEOUT_MS:-1500}"
FUZZ_CONC="${HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY:-1}"

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

if [[ ! -x "$ROOT/bin/workerfuzz" && ! -x "$ROOT/workerfuzz" ]]; then
  echo "[test-fleet] WARN: workerfuzz missing — building ./bin/workerfuzz" >&2
  (cd "$ROOT" && go build -o bin/workerfuzz ./cmd/workerfuzz) || true
fi

mkdir -p "$LOG_DIR" "$SEED_DIR" "$LOG_DIR/locks"
# Drop separate fuzz-only fleet if present (same host should not double-dig).
if [[ -x "$ROOT/scripts/ops/stop_test_named_fuzz_fleet.sh" ]]; then
  bash "$ROOT/scripts/ops/stop_test_named_fuzz_fleet.sh" >/dev/null 2>&1 || true
fi
bash "$ROOT/scripts/ops/stop_test_named_fleet.sh" >/dev/null 2>&1 || true
sleep 1

chmod +x "$ROOT/scripts/ops/named_hybrid_unit.sh"

echo "[test-fleet] starting $N hybrid units (PoH+fuzz=${HYBRID_FUZZ}) GH ${GH_MIN}…${GH_MAX} → $COORD_URL"
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
  : >"$LOG_DIR/${wid}.log"
  : >"$LOG_DIR/${wid}.fuzz.log"
  # Stagger fuzz claims so hub SQLite is less likely to lock.
  stagger_ms=$((i * 150))
  systemd-run --user \
    --unit="$unit" \
    --property=Restart=on-failure \
    --property=RestartSec=5 \
    --working-directory="$ROOT" \
    --setenv=COORD_URL="$COORD_URL" \
    --setenv=COORD_ADMIN_TOKEN="$TOKEN" \
    --setenv=COORD_TOKEN="$TOKEN" \
    --setenv=WORKER_ID="$wid" \
    --setenv=WORKER_NAME="$name" \
    --setenv=BATCH_SIZE="$BATCH_SIZE" \
    --setenv=FORCE_HASHRATE_GHS="$gh" \
    --setenv=HASHRATE_GHS="$gh" \
    --setenv=HACKME_MINER_ED25519_SEED_HEX="$seed" \
    --setenv=MINERSIGN_BIN="$MINERSIGN_BIN" \
    --setenv=LOG_DIR="$LOG_DIR" \
    --setenv=HACKME_NAMED_HYBRID_FUZZ="$HYBRID_FUZZ" \
    --setenv=WORKERFUZZ_HTTP_TIMEOUT_SEC="$HTTP_TIMEOUT_SEC" \
    --setenv=WORKERFUZZ_TIMEOUT_MS="$FUZZ_TIMEOUT_MS" \
    --setenv=HACKME_WORKER_HYBRID_FUZZ_CLAIM_GAP_MS="$FUZZ_GAP_MS" \
    --setenv=HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY="$FUZZ_CONC" \
    /bin/bash -c "sleep $(python3 -c "print(${stagger_ms}/1000)"); exec \"$ROOT/scripts/ops/named_hybrid_unit.sh\""
  echo "$unit" >"$LOG_DIR/${wid}.unit"
  echo "[test-fleet]  $wid  gh=${gh}  hybrid_fuzz=${HYBRID_FUZZ}  unit=${unit}"
done

sleep 4
alive=0
for i in $(seq 0 $((N - 1))); do
  name="${NAMES[$i]:-rig$i}"
  unit="${UNIT_PREFIX}-${name}"
  if systemctl --user is-active --quiet "$unit.service" 2>/dev/null; then
    alive=$((alive + 1))
  fi
done
echo "[test-fleet] active $alive / $N (PoH board + fuzz under same ids)"
echo "[test-fleet] stop: bash scripts/ops/stop_test_named_fleet.sh"
echo "[test-fleet] fuzz logs: $LOG_DIR/worker-*.fuzz.log"
