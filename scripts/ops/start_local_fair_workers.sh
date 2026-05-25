#!/usr/bin/env bash
# Start N pool workers on this host (unique worker_id + hybrid address each).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
N="${1:-3}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
POOL_TOKEN="${POOL_TOKEN:-$(cat "$ROOT/dist/release_0.1.0-rc11h/linux/pool.miner.token")}"
WORKER_BIN="${WORKER_BIN:-$ROOT/bin/workerpoh-cuda}"
[[ -x "$WORKER_BIN" ]] || WORKER_BIN="$ROOT/bin/workerpoh"
mkdir -p "$ROOT/logs/fair-pool"

pkill -f 'workerpoh.*worker-kapa-fair-' 2>/dev/null || true
sleep 1

python3 - "$N" <<'PY' | while read -r wid seed; do
import hashlib, sys
n = int(sys.argv[1])
for i in range(1, n + 1):
    wid = f"worker-kapa-fair-{i}"
    seed = hashlib.sha256(f"fairrun:{i}".encode()).hexdigest()
    print(wid, seed)
PY
  addr="$(HACKME_MINER_ED25519_SEED_HEX="$seed" go run ./cmd/minersign -show-address)"
  echo "[fair-workers] $wid -> $addr"
  HACKME_MINER_ED25519_SEED_HEX="$seed" \
  HACKME_WORKER_SIGN_SUBMITS=1 \
  HACKME_DESKTOP_GPU_POOL=1 \
  HACKME_GPU_FLEET=0 \
  nohup "$WORKER_BIN" \
    -coord "$COORD_URL" \
    -token "$POOL_TOKEN" \
    -worker "$wid" \
    -batch 4194304 \
    -gpu-chunk 4194304 \
    -search-timeout-ms 5000 \
    -gpu-backend cuda \
    >"$ROOT/logs/fair-pool/${wid}.log" 2>&1 &
done
echo "[fair-workers] started $(pgrep -cf 'workerpoh.*worker-kapa-fair-' || echo 0) processes"
