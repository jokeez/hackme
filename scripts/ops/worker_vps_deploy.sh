#!/usr/bin/env bash
# Deploy a pool-only worker rig on a second VPS (no local chain leader).
#
# Architecture:
#   VPS-1 (hackme-vps): canonical node + coordinator  -> https://hackme.tech
#   VPS-2 (this host):  workerpoh + minersign only, submits to coordinator
#
# Prereq on operator machine:
#   - passwordless SSH: WORKER_SSH=root@NEW_VPS_IP
#   - .secrets/hackme_coordinator_admin_token (COORD token)
#   - .secrets/hackme_miner_seed_hex or HACKME_MINER_ED25519_SEED_HEX in env
#
# Usage:
#   WORKER_SSH=root@NEW_VPS \
#   COORD_URL=https://hackme.tech/pool/coordinator \
#   bash scripts/ops/worker_vps_deploy.sh
#
# Optional:
#   WORKER_DEPLOY_DIR=/opt/hackme-worker
#   WORKER_ID=worker-vps2-01
#   SKIP_BUILD=1

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/ops/_deploy_ssh_retry.sh
source "$ROOT/scripts/ops/_deploy_ssh_retry.sh"

WORKER_SSH="${WORKER_SSH:-}"
WORKER_DEPLOY_DIR="${WORKER_DEPLOY_DIR:-/opt/hackme-worker}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
WORKER_ID="${WORKER_ID:-worker-$(echo "${WORKER_SSH:-unknown}" | tr '@.' '-' | tr -cd 'a-zA-Z0-9-')}"
SKIP_BUILD="${SKIP_BUILD:-0}"

SECRET_COORD="${SECRET_COORD:-$ROOT/.secrets/hackme_coordinator_admin_token}"
SECRET_SEED="${SECRET_SEED:-$ROOT/.secrets/hackme_miner_seed_hex}"

if [[ -z "$WORKER_SSH" ]]; then
  echo "[worker-vps-deploy] set WORKER_SSH=root@<second-vps-ip>" >&2
  exit 2
fi

COORD_TOKEN="${COORD_ADMIN_TOKEN:-${HACKME_POOL_COORDINATOR_TOKEN:-}}"
if [[ -z "$COORD_TOKEN" && -f "$SECRET_COORD" ]]; then
  COORD_TOKEN="$(tr -d '\r\n' <"$SECRET_COORD")"
fi
MINER_SEED="${HACKME_MINER_ED25519_SEED_HEX:-}"
if [[ -z "$MINER_SEED" && -f "$SECRET_SEED" ]]; then
  MINER_SEED="$(tr -d '\r\n' <"$SECRET_SEED")"
fi
if [[ -z "$COORD_TOKEN" ]]; then
  echo "[worker-vps-deploy] missing coordinator token (.secrets/hackme_coordinator_admin_token)" >&2
  exit 2
fi
if [[ -z "$MINER_SEED" || ${#MINER_SEED} -ne 64 ]]; then
  echo "[worker-vps-deploy] set HACKME_MINER_ED25519_SEED_HEX (64 hex) or .secrets/hackme_miner_seed_hex" >&2
  exit 2
fi

if [[ "$SKIP_BUILD" != "1" ]]; then
  echo "[worker-vps-deploy] build workerpoh (+ opencl if available) and minersign"
  gpu_tags=""
  if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then
    gpu_tags="opencl"
  fi
  if [[ -n "$gpu_tags" ]]; then
    go build -trimpath -tags "$gpu_tags" -o "$ROOT/workerpoh-opencl" ./cmd/workerpoh
    deploy_bin="workerpoh-opencl"
  else
    go build -trimpath -o "$ROOT/workerpoh" ./cmd/workerpoh
    deploy_bin="workerpoh"
  fi
  go build -trimpath -o "$ROOT/minersign" ./cmd/minersign
else
  deploy_bin="workerpoh-opencl"
  [[ -f "$ROOT/workerpoh-opencl" ]] || deploy_bin="workerpoh"
fi

echo "[worker-vps-deploy] rsync worker bundle -> $WORKER_SSH:$WORKER_DEPLOY_DIR"
deploy_ssh_retry_run ssh "$WORKER_SSH" "mkdir -p '$WORKER_DEPLOY_DIR'/logs '$WORKER_DEPLOY_DIR'/scripts/ops"
deploy_ssh_retry_run rsync -az \
  "$ROOT/$deploy_bin" "$ROOT/minersign" \
  "$ROOT/scripts/ops/worker_autostart.sh" \
  "$ROOT/scripts/ops/worker_loop.sh" \
  "$ROOT/scripts/ops/run_public_worker_smoke.sh" \
  "$WORKER_SSH:$WORKER_DEPLOY_DIR/"

deploy_ssh_retry_run ssh "$WORKER_SSH" bash -s <<REMOTE
set -euo pipefail
d='$WORKER_DEPLOY_DIR'
cd "\$d"
chmod 755 workerpoh* minersign worker_autostart.sh worker_loop.sh run_public_worker_smoke.sh 2>/dev/null || true
[[ -f workerpoh-opencl ]] && ln -sf workerpoh-opencl workerpoh || true
cat > .env.worker <<EOF
COORD_URL=$COORD_URL
COORD_TOKEN=$COORD_TOKEN
COORD_ADMIN_TOKEN=$COORD_TOKEN
WORKER_ID=$WORKER_ID
HACKME_MINER_ED25519_SEED_HEX=$MINER_SEED
BATCH_SIZE=4194304
HACKME_GPU_BACKEND=auto
EOF
chmod 600 .env.worker
pkill -f "worker_autostart.sh" 2>/dev/null || true
sleep 1
set -a
source .env.worker
set +a
export WORKER_BIN="\$d/workerpoh"
nohup bash "\$d/worker_autostart.sh" >"\$d/logs/worker_autostart.log" 2>&1 &
sleep 3
echo "[remote] worker tail:"
tail -20 "\$d/logs/worker_autostart.log" 2>/dev/null || true
echo "[remote] coordinator probe:"
curl -fsS --max-time 15 "${COORD_URL%/}/api/work/stats" | head -c 300 || echo "coord stats failed"
echo
REMOTE

echo "[worker-vps-deploy] done. Logs: ssh $WORKER_SSH tail -f $WORKER_DEPLOY_DIR/logs/worker_autostart.log"
echo "[worker-vps-deploy] smoke: COORD_URL=$COORD_URL WORKER_ID=$WORKER_ID bash scripts/ops/run_public_worker_smoke.sh"
