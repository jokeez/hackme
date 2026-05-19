#!/usr/bin/env bash
# Generate worker-scoped coordinator token, install on hub + remote workers (not admin).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NODE_SSH="${NODE_SSH:-hackme-vps}"
MSK_SSH="${MSK_SSH:-root@82.146.53.7}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
MSK_DEPLOY="${MSK_DEPLOY_DIR:-/opt/hackme-worker}"
WORKER_TOKEN_FILE="${WORKER_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_worker_token}"

log() { echo "[worker-token] $*"; }

if [[ ! -f "$WORKER_TOKEN_FILE" ]]; then
  bash "$ROOT/scripts/ops/gen_coordinator_worker_token.sh" "$WORKER_TOKEN_FILE"
fi
WORKER_TOKEN="$(tr -d '\r\n' <"$WORKER_TOKEN_FILE")"
[[ -n "$WORKER_TOKEN" ]] || { log "empty token in $WORKER_TOKEN_FILE" >&2; exit 2; }

log "hub $NODE_SSH — set HACKME_COORDINATOR_WORKER_TOKEN in .env.coord"
ssh -o BatchMode=yes -o ConnectTimeout=20 "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
ENV='$DEPLOY/.env.coord'
touch "\$ENV"
if grep -q '^HACKME_COORDINATOR_WORKER_TOKEN=' "\$ENV" 2>/dev/null; then
  sed -i 's|^HACKME_COORDINATOR_WORKER_TOKEN=.*|HACKME_COORDINATOR_WORKER_TOKEN=${WORKER_TOKEN}|' "\$ENV"
else
  echo 'HACKME_COORDINATOR_WORKER_TOKEN=${WORKER_TOKEN}' >>"\$ENV"
fi
grep '^HACKME_COORDINATOR_WORKER_TOKEN=' "\$ENV" | sed 's/=.*/=***redacted***/'
systemctl restart hackme-coordinator 2>/dev/null || true
sleep 2
systemctl is-active hackme-coordinator 2>/dev/null || true
REMOTE

log "MSK $MSK_SSH — COORD_TOKEN=worker (only after hub restart above)"
ssh -o BatchMode=yes -o ConnectTimeout=15 "$MSK_SSH" "bash -s" <<REMOTE
set -euo pipefail
# Probe hub accepts worker token before switching MSK (avoid 401 downtime).
probe="\$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -H 'X-Hackme-Admin-Token: ${WORKER_TOKEN}' \
  -d '{"worker_id":"vps-canary-01","batch_size":1024}' \
  'https://hackme.tech/pool/coordinator/api/work/claim' 2>/dev/null || echo 000)"
if [[ "\$probe" != "200" ]]; then
  echo "hub not ready for worker token (claim HTTP \$probe) — skip MSK token swap" >&2
  exit 0
fi
ENV='$MSK_DEPLOY/.env.worker'
UNIT=/etc/systemd/system/hackme-worker.service
touch "\$ENV"
set_kv() { k="\$1"; v="\$2"; grep -q "^\${k}=" "\$ENV" && sed -i "s|^\${k}=.*|\${k}=\${v}|" "\$ENV" || echo "\${k}=\${v}" >>"\$ENV"; }
set_kv COORD_TOKEN '${WORKER_TOKEN}'
set_kv COORD_ADMIN_TOKEN '${WORKER_TOKEN}'
set_kv WORKER_ID worker-vps-msk-01
if [[ -f "\$UNIT" ]]; then
  sed -i 's|-token [^ ]*|-token ${WORKER_TOKEN}|g' "\$UNIT"
  sed -i 's|HMC_ADMIN_[^ ]*|${WORKER_TOKEN}|g' "\$UNIT" 2>/dev/null || true
  systemctl daemon-reload
  systemctl restart hackme-worker
  sleep 3
  systemctl is-active hackme-worker
fi
REMOTE

log "verify worker token cannot clear-abuse (expect 401/403)"
code="\$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  -H 'X-Hackme-Admin-Token: ${WORKER_TOKEN}' \
  'https://hackme.tech/pool/coordinator/api/work/clear-abuse' 2>/dev/null || echo 000)"
log "clear-abuse with worker token: HTTP \$code (want 401 or 403)"

log "verify worker token can claim (expect 200)"
claim_code="\$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  -H 'Content-Type: application/json' \
  -H 'X-Hackme-Admin-Token: ${WORKER_TOKEN}' \
  -d '{"worker_id":"vps-canary-01","batch_size":1024}' \
  'https://hackme.tech/pool/coordinator/api/work/claim' 2>/dev/null || echo 000)"
log "claim with worker token: HTTP \$claim_code (want 200)"

log "done — desktop keeps admin token locally; remote rigs use $WORKER_TOKEN_FILE"
