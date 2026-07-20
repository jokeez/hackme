#!/usr/bin/env bash
# Ensure integrator env on canonical node (.env.vps).
# Self-register defaults OFF (fail-closed). Opt-in: INTEGRATOR_SELF_REGISTER=1.
set -euo pipefail
NODE_SSH="${NODE_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
SELF_REG="${INTEGRATOR_SELF_REGISTER:-0}"
case "$(printf '%s' "$SELF_REG" | tr '[:upper:]' '[:lower:]')" in
  1|true|on|yes) SELF_REG=1 ;;
  *) SELF_REG=0 ;;
esac
ssh -o BatchMode=yes "$NODE_SSH" "DEPLOY='$NODE_DEPLOY_DIR' SELF_REG='$SELF_REG' bash -s" <<'REMOTE'
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
append_kv HACKME_INTEGRATOR_SELF_REGISTER "$SELF_REG"
append_kv HACKME_INTEGRATOR_MAX_TOKENS 200
echo "[integrator-env] HACKME_INTEGRATOR_SELF_REGISTER=$SELF_REG"
sudo systemctl restart hackme-node
sleep 2
systemctl is-active hackme-node
REMOTE
