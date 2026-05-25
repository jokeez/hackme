#!/usr/bin/env bash
# Apply miner-friendly pool settings: fair lease caps, longer leases, canary throttle, full payout map.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WALLET="${WALLET:-HMC-91fe007e4036c602}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
SSH_KEY="${SSH_KEY:-${HOME}/.ssh/id_ed25519}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
PAYOUT_MAP="worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET},vps-canary-01=${WALLET},worker-vps-62-01=${WALLET}"

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

log "ensure VPS chain command node can produce blocks (pool workers do not append chain height)"
ssh "${ssh_opts[@]}" "$NODE_SSH" "bash -s" <<'REMOTE'
set -euo pipefail
ENV='/opt/hackme/.env.vps'
touch "$ENV"
if grep -q '^HACKME_CHAIN_LEADER_LOCAL_POH=' "$ENV" 2>/dev/null; then
  sed -i 's/^HACKME_CHAIN_LEADER_LOCAL_POH=.*/HACKME_CHAIN_LEADER_LOCAL_POH=1/' "$ENV"
else
  echo 'HACKME_CHAIN_LEADER_LOCAL_POH=1' >>"$ENV"
fi
# Open orders: pool workers only (no local leader grabbing escrow on command node).
if grep -q '^HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY=' "$ENV" 2>/dev/null; then
  sed -i 's/^HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY=.*/HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY=1/' "$ENV"
else
  echo 'HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY=1' >>"$ENV"
fi
grep -E '^HACKME_CHAIN_LEADER_(LOCAL_POH|ORDERS_VIA_POOL_ONLY)=' "$ENV" || true
REMOTE

log "build coordinator + worker on VPS"
ssh "${ssh_opts[@]}" "$NODE_SSH" "cd '$DEPLOY' && go build -o bin/coordinator ./cmd/coordinator && go build -o bin/workerpoh ./cmd/workerpoh && \
  sudo systemctl stop hackme-coordinator 2>/dev/null || true; \
  sudo cp bin/coordinator '$DEPLOY/coordinator' && \
  sudo cp bin/workerpoh '$DEPLOY/workerpoh' 2>/dev/null || cp bin/workerpoh '$DEPLOY/workerpoh'"

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
set_kv HACKME_COORDINATOR_MAX_ACTIVE_LEASES_PER_WORKER 12
set_kv HACKME_COORDINATOR_CLAIM_PER_MIN 600
set_kv HACKME_COORDINATOR_SUBMIT_PER_MIN 3000
set_kv HACKME_COORDINATOR_ORDERS_PRIORITY 1
# Fair public pool: pay for accepted attempts (capped by batch), not only rare found hits.
set_kv HACKME_COORDINATOR_PAYOUT_FOUND_ONLY 0
set_kv HACKME_COORDINATOR_REWARD_AUTO 1
set_kv HACKME_POOL_HYBRID_SIGNER_ENABLED 1
set_kv HACKME_POOL_HYBRID_SIGNER_STRICT 1
set_kv HACKME_POOL_HYBRID_REQUIRE_FOUND_SIG 1
# Pool difficulty bounds: M scales ~ target_mod_min per GH/s; raise max so large fleets are not stuck at cap.
set_kv HACKME_COORDINATOR_POOL_TARGET_MOD_MIN 2000000
set_kv HACKME_COORDINATOR_POOL_TARGET_MOD_MAX 1000000000
NODE_ADMIN=\$(grep '^HACKME_ADMIN_TOKEN=' '$DEPLOY/.env.vps' 2>/dev/null | cut -d= -f2- || true)
if [[ -n "\$NODE_ADMIN" ]]; then
  set_kv HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN "\$NODE_ADMIN"
fi
grep -E 'LEASE|ACTIVE_LEASES|CLAIM|SUBMIT|ORDERS|PAYOUT|REWARD|HYBRID|TARGET_MOD|ORDERS_ADMIN' "\$ENV" | tail -18
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

log "restart node + coordinator + canary worker"
ssh "${ssh_opts[@]}" "$NODE_SSH" "sudo systemctl restart hackme-node.service hackme-coordinator.service hackme-workerpoh.service 2>/dev/null || \
  (systemctl restart hackme-node hackme-coordinator hackme-workerpoh 2>/dev/null || true)"

sleep 4
log "run settlement"
ssh "${ssh_opts[@]}" "$NODE_SSH" "cd '$DEPLOY' && set -a && source .env.settlement && set +a && \
  FORCE_SETTLE_ALL=1 bash scripts/ops/settle_worker_payouts.sh" 2>&1 | tail -20

log "done — run: bash scripts/ops/miner_happiness_check.sh"
