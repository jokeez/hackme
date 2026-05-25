#!/usr/bin/env bash
# Enable automatic integrator token issuance on canonical node (.env.vps).
set -euo pipefail
NODE_SSH="${NODE_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
ssh -o BatchMode=yes "$NODE_SSH" "DEPLOY='$NODE_DEPLOY_DIR' bash -s" <<'REMOTE'
set -euo pipefail
ENV="$DEPLOY/.env.vps"
[[ -f "$ENV" ]] || { echo "missing $ENV" >&2; exit 1; }
append_kv() {
  local key="$1" val="$2"
  if grep -q "^${key}=" "$ENV" 2>/dev/null; then
    sudo sed -i "s|^${key}=.*|${key}=${val}|" "$ENV"
  else
    echo "${key}=${val}" | sudo tee -a "$ENV" >/dev/null
  fi
}
append_kv HACKME_INTEGRATOR_SELF_REGISTER 1
append_kv HACKME_INTEGRATOR_MAX_TOKENS 200
echo "[integrator-env] HACKME_INTEGRATOR_SELF_REGISTER=1"
sudo systemctl restart hackme-node
sleep 2
systemctl is-active hackme-node
REMOTE
