#!/usr/bin/env bash
# Run workerfuzz alongside PoH — claims pool fuzz work from coordinator.
#
#   bash scripts/ops/workerfuzz_autostart.sh
#   WORKER_ID=worker-kapa-pc-fuzz bash scripts/ops/workerfuzz_autostart.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

if [[ -f "$ROOT/.env.desktop" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env.desktop"
  set +a
fi

COORD_URL="${COORD_URL:-${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}}"
COORD_URL="${COORD_URL%/}"
export COORD_URL
export COORD_TOKEN="${COORD_TOKEN:-$(tr -d '\r\n' <"${ROOT}/.secrets/hackme_coordinator_worker_token" 2>/dev/null || true)}"
export HACKME_MINER_ED25519_SEED_HEX="${HACKME_MINER_ED25519_SEED_HEX:-$(tr -d '\r\n' <"${ROOT}/.secrets/hackme_treasury_ed25519_seed.hex" 2>/dev/null || true)}"
export WORKER_ID="${WORKER_ID:-worker-$(hostname -s 2>/dev/null || echo local)-fuzz}"
export WORKERFUZZ_HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-120}"

LOG="$ROOT/logs/workerfuzz-autostart.log"
mkdir -p "$ROOT/logs"

if [[ -z "$COORD_TOKEN" || -z "$HACKME_MINER_ED25519_SEED_HEX" ]]; then
  echo "[workerfuzz-autostart] missing COORD_TOKEN or HACKME_MINER_ED25519_SEED_HEX" >&2
  exit 1
fi

echo "[workerfuzz-autostart] coord=$COORD_URL worker=$WORKER_ID" | tee -a "$LOG"
exec >>"$LOG" 2>&1

backoff=2
while true; do
  if go run ./cmd/workerfuzz -worker "$WORKER_ID" -timeout-ms 800; then
    backoff=2
  else
    echo "[workerfuzz-autostart] exit $? — backoff ${backoff}s"
    sleep "$backoff"
    backoff=$((backoff * 2))
    [[ "$backoff" -gt 60 ]] && backoff=60
  fi
done
