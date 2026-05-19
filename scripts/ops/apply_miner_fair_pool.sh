#!/usr/bin/env bash
# Apply miner-friendly pool settings: fair lease caps, longer leases, canary throttle, full payout map.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WALLET="${WALLET:-HMC-91fe007e4036c602}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SSH_KEY="${SSH_KEY:-${HOME}/.ssh/id_ed25519}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
PAYOUT_MAP="worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET},vps-canary-01=${WALLET}"

ssh_opts=(-o BatchMode=yes -o ConnectTimeout=12)
[[ -f "$SSH_KEY" ]] && ssh_opts+=(-i "$SSH_KEY")

log() { echo "[miner-fair] $*"; }

log "sync repo to ${NODE_SSH}:${DEPLOY} (coordinator + worker + scripts)"
rsync -az --delete \
  --exclude '.git/' --exclude 'data/' --exclude 'reports/' --exclude 'logs/' \
  --exclude '.env' --exclude '.env.*' --exclude '.secrets/' \
  --exclude '.cargo/' --exclude '.npm-global/' --exclude '.rustup/' \
  --exclude 'tinygo/' --exclude 'node_modules/' \
  -e "ssh ${ssh_opts[*]}" \
  "$ROOT/cmd" "$ROOT/scripts" "$ROOT/internal" "$ROOT/go.mod" "$ROOT/go.sum" \
  "$ROOT/main.go" "$ROOT/pool.go" "$ROOT/metrics.go" "$ROOT/fuzz_campaigns.go" "$ROOT/fuzz_runner.go" \
  "$ROOT/task_codegen.go" "$ROOT/toolchain_env.go" \
  "${NODE_SSH}:${DEPLOY}/" 2>/dev/null || rsync -az \
  --exclude '.git/' --exclude 'data/' --exclude 'reports/' --exclude 'logs/' \
  --exclude '.env' --exclude '.env.*' --exclude '.secrets/' \
  --exclude '.cargo/' --exclude '.npm-global/' --exclude '.rustup/' \
  --exclude 'tinygo/' --exclude 'node_modules/' \
  -e "ssh ${ssh_opts[*]}" \
  "$ROOT/" "${NODE_SSH}:${DEPLOY}/"

log "build coordinator + worker on VPS"
ssh "${ssh_opts[@]}" "$NODE_SSH" "cd '$DEPLOY' && go build -o bin/coordinator ./cmd/coordinator && go build -o bin/workerpoh ./cmd/workerpoh"

log "patch .env.coord (fair leases + longer deadline)"
ssh "${ssh_opts[@]}" "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
ENV='$DEPLOY/.env.coord'
touch "\$ENV"
set_kv() {
  local k="\$1" v="\$2"
  if grep -q "^\${k}=" "\$ENV" 2>/dev/null; then
    sed -i "s|^\${k}=.*|\${k}=\${v}|" "\$ENV"
  else
    echo "\${k}=\${v}" >>"\$ENV"
  fi
}
set_kv HACKME_COORDINATOR_LEASE_SEC 90
set_kv HACKME_COORDINATOR_MAX_ACTIVE_LEASES_PER_WORKER 3
set_kv HACKME_COORDINATOR_CLAIM_PER_MIN 600
set_kv HACKME_COORDINATOR_SUBMIT_PER_MIN 3000
set_kv HACKME_COORDINATOR_ORDERS_PRIORITY 1
# Fair public pool: pay for accepted attempts (capped by batch), not only rare found hits.
set_kv HACKME_COORDINATOR_PAYOUT_FOUND_ONLY 0
set_kv HACKME_COORDINATOR_REWARD_AUTO 1
set_kv HACKME_POOL_HYBRID_SIGNER_ENABLED 1
set_kv HACKME_POOL_HYBRID_SIGNER_STRICT 1
set_kv HACKME_POOL_HYBRID_REQUIRE_FOUND_SIG 1
grep -E 'LEASE|ACTIVE_LEASES|CLAIM|SUBMIT|ORDERS|PAYOUT|REWARD|HYBRID' "\$ENV" | tail -14
REMOTE

log "patch .env.worker canary throttle"
ssh "${ssh_opts[@]}" "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
ENV='$DEPLOY/.env.worker'
touch "\$ENV"
set_kv() {
  local k="\$1" v="\$2"
  if grep -q "^\${k}=" "\$ENV" 2>/dev/null; then
    sed -i "s|^\${k}=.*|\${k}=\${v}|" "\$ENV"
  else
    echo "\${k}=\${v}" >>"\$ENV"
  fi
}
set_kv HACKME_WORKER_CLAIM_COOLDOWN_MS 1500
set_kv BATCH_SIZE 524288
set_kv HACKME_WORKER_CLAIM_TIMEOUT 35s
set_kv HACKME_WORKER_SUBMIT_TIMEOUT 90s
grep -E 'COOLDOWN|BATCH|WORKER_ID|CLAIM|SUBMIT' "\$ENV" | tail -10
REMOTE

log "patch settlement payout map (all rigs → ${WALLET})"
ssh "${ssh_opts[@]}" "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
ENV='$DEPLOY/.env.settlement'
touch "\$ENV"
if grep -q '^WORKER_PAYOUT_MAP=' "\$ENV"; then
  sed -i 's|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=${PAYOUT_MAP}|' "\$ENV"
else
  echo 'WORKER_PAYOUT_MAP=${PAYOUT_MAP}' >>"\$ENV"
fi
grep WORKER_PAYOUT_MAP "\$ENV"
REMOTE

log "restart coordinator + canary worker"
ssh "${ssh_opts[@]}" "$NODE_SSH" "sudo systemctl restart hackme-coordinator.service hackme-workerpoh.service 2>/dev/null || \
  (systemctl restart hackme-coordinator hackme-workerpoh 2>/dev/null || true)"

sleep 4
log "run settlement"
ssh "${ssh_opts[@]}" "$NODE_SSH" "cd '$DEPLOY' && set -a && source .env.settlement && set +a && \
  FORCE_SETTLE_ALL=1 bash scripts/ops/settle_worker_payouts.sh" 2>&1 | tail -20

log "done — run: bash scripts/ops/miner_happiness_check.sh"
