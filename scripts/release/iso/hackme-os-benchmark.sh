#!/usr/bin/env bash
# HackMe OS — 60s local mining throughput probe (before/after tune comparison).
set -euo pipefail

ROOT="${HACKME_ROOT:-/opt/hackme}"
DURATION="${1:-60}"
ENV_STATE="/var/lib/hackme/miner.env"
[[ -f /etc/hackme/miner.env ]] && source /etc/hackme/miner.env
[[ -f "$ENV_STATE" ]] && source "$ENV_STATE"
[[ -f /var/lib/hackme/rig.env ]] && source /var/lib/hackme/rig.env

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
if [[ -z "${COORD_TOKEN:-}" && -f /etc/hackme/pool.token ]]; then
  COORD_TOKEN="$(tr -d '\r\n' </etc/hackme/pool.token)"
fi

if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
  echo "Run hackme-miner-firstboot first (miner seed missing)" >&2
  exit 1
fi

RUNNER="${ROOT}/scripts/release/iso/run-miner-worker.sh"
if [[ ! -x "$RUNNER" ]]; then
  echo "HackMe OS payload missing under ${ROOT}" >&2
  exit 1
fi

echo "=== HackMe OS benchmark (${DURATION}s) ==="
if [[ -f /run/hackme-os/topology.json ]]; then
  echo "topology: $(cat /run/hackme-os/topology.json)"
fi
if [[ -n "${HACKME_RIG_PROFILE:-}" ]]; then
  echo "rig profile: ${HACKME_RIG_PROFILE}"
fi

BEFORE="$(curl -fsS --max-time 8 "${COORD_URL}/api/work/stats" 2>/dev/null | jq -r '.summary.pool_hashrate_gh_s // .pool_hashrate_gh_s // 0' 2>/dev/null || echo 0)"
echo "pool GH/s (before): ${BEFORE}"

LOG="$(mktemp /tmp/hackme-os-bench.XXXXXX.log)"
echo "Starting worker probe → ${LOG}"
timeout --signal=INT "${DURATION}" "$RUNNER" 2>&1 | tee "$LOG" || true

ATTEMPTS="$(grep -cE 'attempt|submit|found|Search' "$LOG" 2>/dev/null || echo 0)"
echo "---"
echo "log lines (mining): ${ATTEMPTS}"
echo "tail:"
tail -8 "$LOG" 2>/dev/null || true

AFTER="$(curl -fsS --max-time 8 "${COORD_URL}/api/work/stats" 2>/dev/null | jq -r '.summary.pool_hashrate_gh_s // .pool_hashrate_gh_s // 0' 2>/dev/null || echo 0)"
echo "pool GH/s (after):  ${AFTER}"
echo "Done. Register worker on coordinator for payout: ${WORKER_ID:-unknown}"
