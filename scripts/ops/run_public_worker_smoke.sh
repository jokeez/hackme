#!/usr/bin/env bash
# Short pool worker smoke against public TLS coordinator (default https://hackme.tech/pool/coordinator).
# Uses worker_autostart.sh + workerpoh (GPU when available) with hybrid signing when seed is present.
#
# Requires:
#   .secrets/hackme_coordinator_admin_token — one line (VPS HACKME_COORDINATOR_ADMIN_TOKEN)
#   .secrets/hackme_miner_ed25519_seed.hex or data/miner_submit_ed25519_seed.hex — for signed submits
#
# Exit 0: PASS or SKIP (no coordinator secret). Exit 1: worker ran but no successful submit.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
COORD_SECRET="${ROOT_DIR}/.secrets/hackme_coordinator_admin_token"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[public-worker-smoke] missing: $1" >&2
    exit 1
  }
}
require_cmd bash
require_cmd timeout

if [[ ! -f "$COORD_SECRET" ]]; then
  echo "[public-worker-smoke] SKIP: no $COORD_SECRET (copy VPS coordinator admin token, one line)" >&2
  exit 0
fi

SMOKE_SEC="${PUBLIC_WORKER_SMOKE_SEC:-55}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
WORKER_ID="${WORKER_ID:-public-smoke-$(date -u +%Y%m%dT%H%M%SZ)}"
BATCH_SIZE="${BATCH_SIZE:-4194304}"

export COORD_URL
export COORD_TOKEN
COORD_TOKEN="$(head -n1 "$COORD_SECRET" | tr -d '\r\n')"
export COORD_ADMIN_TOKEN="$COORD_TOKEN"
export WORKER_ID
export BATCH_SIZE
export HACKME_REPO_ROOT="$ROOT_DIR"
export HACKME_GPU_FLEET=0

seed=""
for f in \
  "${ROOT_DIR}/data/miner_submit_ed25519_seed.hex" \
  "${ROOT_DIR}/.secrets/hackme_miner_ed25519_seed.hex"; do
  if [[ -f "$f" ]]; then
    seed="$(tr -d ' \r\n' <"$f")"
    break
  fi
done
if [[ -z "$seed" ]] && command -v go >/dev/null 2>&1; then
  seed="$(cd "$ROOT_DIR" && go run ./cmd/minersign -gen-seed 2>/dev/null | tr -d ' \r\n' || true)"
  if [[ -n "$seed" ]]; then
    mkdir -p "${ROOT_DIR}/.secrets"
    printf '%s\n' "$seed" >"${ROOT_DIR}/.secrets/hackme_miner_ed25519_seed.hex"
    chmod 600 "${ROOT_DIR}/.secrets/hackme_miner_ed25519_seed.hex" 2>/dev/null || true
    echo "[public-worker-smoke] generated miner seed -> .secrets/hackme_miner_ed25519_seed.hex"
  fi
fi
if [[ -z "$seed" ]]; then
  echo "[public-worker-smoke] FAIL: miner seed required for public pool (hybrid signer)" >&2
  echo "[public-worker-smoke] run: go run ./cmd/minersign -gen-seed > .secrets/hackme_miner_ed25519_seed.hex" >&2
  exit 1
fi
export HACKME_MINER_ED25519_SEED_HEX="$seed"

log="$(mktemp)"
trap 'rm -f "$log"' EXIT

echo "[public-worker-smoke] coord=$COORD_URL worker=$WORKER_ID (${SMOKE_SEC}s cap, worker_autostart)"

set +e
timeout "$SMOKE_SEC" bash "$ROOT_DIR/scripts/ops/worker_autostart.sh" >"$log" 2>&1
ec=$?
set -e

if grep -qE 'submit ok found=' "$log"; then
  echo "[public-worker-smoke] PASS (GPU PoH worker + hybrid sign)"
  grep -E 'workerpoh:|submit ok|searcher=' "$log" | tail -12
  exit 0
fi
if grep -qE '\[worker-autostart\] worker=.*exited rc=0' "$log" && grep -qE 'submit ok' "$log"; then
  echo "[public-worker-smoke] PASS (autostart)"
  tail -12 "$log"
  exit 0
fi

echo "[public-worker-smoke] FAIL: no successful submit (exit=$ec); tail:" >&2
tail -45 "$log" >&2
exit 1
