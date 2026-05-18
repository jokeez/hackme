#!/usr/bin/env bash
set -euo pipefail

# Canonical VPS one-shot setup:
# - configures settlement env for daily+threshold payout policy
# - enables settlement + settlement-health systemd timers
# - runs immediate settlement + healthcheck + endpoint smoke
#
# Run on canonical VPS as root:
#   sudo bash scripts/ops/canonical_autopilot_setup.sh

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[canonical-autopilot] missing command: $1" >&2
    exit 1
  }
}

require_cmd jq
require_cmd curl
require_cmd python3
require_cmd systemctl

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "[canonical-autopilot] run as root (use sudo)" >&2
  exit 1
fi

ROOT_DIR="${HACKME_ROOT:-/opt/hackme}"
ENV_SETTLE="${ENV_SETTLE:-${ROOT_DIR}/.env.settlement}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"

# SLA defaults:
MIN_SETTLE_HMC="${MIN_SETTLE_HMC:-0.01}"
DAILY_FORCE_INTERVAL_SEC="${DAILY_FORCE_INTERVAL_SEC:-86400}"
DAILY_MIN_SETTLE_HMC="${DAILY_MIN_SETTLE_HMC:-0.0001}"
MAX_UNSETTLED_HMC="${MAX_UNSETTLED_HMC:-0.5}"
MAX_SWEEP_ETA_SEC="${MAX_SWEEP_ETA_SEC:-93600}"
EXPECTED_WALLET_SOURCES="${EXPECTED_WALLET_SOURCES:-canonical_peer,local_db}"
STATE_FILE="${STATE_FILE:-${ROOT_DIR}/data/worker_settlement_state.json}"

ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
if [[ -z "$ADMIN_TOKEN" && -f "${ROOT_DIR}/.env.vps" ]]; then
  ADMIN_TOKEN="$(
    awk -F= '$1=="HACKME_ADMIN_TOKEN"{print $2}' "${ROOT_DIR}/.env.vps" | tail -n 1 | tr -d '"' | xargs
  )"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[canonical-autopilot] ADMIN_TOKEN is required (or set HACKME_ADMIN_TOKEN / .env.vps)" >&2
  exit 2
fi

if [[ ! -d "$ROOT_DIR/scripts/ops" ]]; then
  echo "[canonical-autopilot] scripts not found under ${ROOT_DIR}" >&2
  exit 3
fi

mkdir -p "${ROOT_DIR}/data" "${ROOT_DIR}/logs"

if [[ ! -f "$ENV_SETTLE" ]]; then
  cp "${ROOT_DIR}/scripts/ops/settlement.env.example" "$ENV_SETTLE"
  chmod 600 "$ENV_SETTLE"
fi

python3 - "$ENV_SETTLE" \
  "$COORD_URL" "$CHAIN_BASE" "$LOCAL_BASE" "$ADMIN_TOKEN" \
  "$MIN_SETTLE_HMC" "$DAILY_FORCE_INTERVAL_SEC" "$DAILY_MIN_SETTLE_HMC" \
  "$MAX_UNSETTLED_HMC" "$MAX_SWEEP_ETA_SEC" "$EXPECTED_WALLET_SOURCES" "$STATE_FILE" <<'PY'
import sys
from pathlib import Path

env_path = Path(sys.argv[1])
updates = {
    "COORD_URL": sys.argv[2],
    "CHAIN_BASE": sys.argv[3],
    "LOCAL_BASE": sys.argv[4],
    "ADMIN_TOKEN": sys.argv[5],
    "MIN_SETTLE_HMC": sys.argv[6],
    "DAILY_FORCE_INTERVAL_SEC": sys.argv[7],
    "DAILY_MIN_SETTLE_HMC": sys.argv[8],
    "MAX_UNSETTLED_HMC": sys.argv[9],
    "MAX_SWEEP_ETA_SEC": sys.argv[10],
    "EXPECTED_WALLET_SOURCES": sys.argv[11],
    "STATE_FILE": sys.argv[12],
}

lines = []
if env_path.exists():
    lines = env_path.read_text(encoding="utf-8").splitlines()

kv = {}
raw = []
for line in lines:
    s = line.strip()
    if not s or s.startswith("#") or "=" not in line:
        raw.append(line)
        continue
    k, v = line.split("=", 1)
    kv[k.strip()] = v.strip()

for k, v in updates.items():
    kv[k] = v

preserve = [line for line in raw if line.strip().startswith("#") or not line.strip()]
out = []
if preserve:
    out.extend(preserve)
if out and out[-1].strip():
    out.append("")
for k in sorted(kv):
    out.append(f"{k}={kv[k]}")
out.append("")
env_path.write_text("\n".join(out), encoding="utf-8")
PY

chown hackme:hackme "$ENV_SETTLE" || true

echo "[canonical-autopilot] enabling settlement timers"
bash "${ROOT_DIR}/scripts/ops/setup_worker_settlement_service.sh"

echo "[canonical-autopilot] immediate settlement run"
set +e
set -a
# shellcheck disable=SC1090
source "$ENV_SETTLE"
set +a
bash "${ROOT_DIR}/scripts/ops/settle_worker_payouts.sh"
settle_rc=$?
bash "${ROOT_DIR}/scripts/ops/settlement_healthcheck.sh"
health_rc=$?
set -e

echo "[canonical-autopilot] endpoint smoke"
status_json="$(curl -fsS --max-time 15 "${CHAIN_BASE}/api/status")"
workers_json="$(curl -fsS --max-time 15 "${COORD_URL}/api/work/stats?details=1")"
settle_json="$(curl -fsS --max-time 15 "${LOCAL_BASE}/api/worker/settlement")"

tip_h="$(printf '%s' "$status_json" | jq -r '.tip_height // 0')"
mining="$(printf '%s' "$status_json" | jq -r '.mining // false')"
workers_count="$(printf '%s' "$workers_json" | jq -r '(.workers // {} | length)')"
unpaid_hmc="$(printf '%s' "$settle_json" | jq -r '.total_unpaid_hmc // 0')"
eta_sec="$(printf '%s' "$settle_json" | jq -r '.daily_sweep_eta_sec // 0')"

echo "[canonical-autopilot] status tip_height=${tip_h} mining=${mining}"
echo "[canonical-autopilot] work workers_count=${workers_count}"
echo "[canonical-autopilot] settlement unpaid_hmc=${unpaid_hmc} sweep_eta_sec=${eta_sec}"

if [[ "$settle_rc" -ne 0 || "$health_rc" -ne 0 ]]; then
  echo "[canonical-autopilot] WARN: immediate settlement/healthcheck returned non-zero (settle=${settle_rc} health=${health_rc})" >&2
fi

echo "[canonical-autopilot] DONE"
echo "[canonical-autopilot] env=${ENV_SETTLE}"
echo "[canonical-autopilot] monitor:"
echo "  systemctl list-timers --all | rg 'hackme-worker-settlement'"
echo "  journalctl -u hackme-worker-settlement.service -n 80 --no-pager"
echo "  journalctl -u hackme-worker-settlement-healthcheck.service -n 80 --no-pager"
