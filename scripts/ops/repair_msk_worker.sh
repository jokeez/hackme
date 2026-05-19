#!/usr/bin/env bash
# Restart / redeploy Moscow pool worker (worker-vps-msk-01).
# Passwordless SSH preferred; else: MSK_SSH_PASSWORD=... (never commit).
#
#   MSK_SSH=root@82.146.53.7 MSK_SSH_PASSWORD='...' bash scripts/ops/repair_msk_worker.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export MSK_HOST="${MSK_HOST:-82.146.53.7}"
export MSK_SSH="${MSK_SSH:-root@82.146.53.7}"
export MSK_DEPLOY_DIR="${MSK_DEPLOY_DIR:-/opt/hackme-worker}"
export WORKER_ID="${WORKER_ID:-worker-vps-msk-01}"
export COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
export WALLET="${WALLET:-HMC-91fe007e4036c602}"
export SECRET_COORD="${SECRET_COORD:-$ROOT/.secrets/hackme_coordinator_admin_token}"
export SECRET_SEED="${SECRET_SEED:-$ROOT/data/miner_submit_ed25519_seed.hex}"

go build -trimpath -o /tmp/workerpoh-msk "$ROOT/cmd/workerpoh"
go build -trimpath -o /tmp/minersign-msk "$ROOT/cmd/minersign"

python3 "$ROOT/scripts/ops/_repair_msk_worker.py"

echo "[msk-repair] active rigs:"
curl -fsS "https://hackme.tech/api/global/metrics" | python3 -c \
  "import sys,json; d=json.load(sys.stdin); print([(r['worker_id'],round(r.get('hashrate_gh_s',0),2)) for r in d.get('network',{}).get('active_rigs',[])])" || true
