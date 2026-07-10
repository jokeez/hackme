#!/usr/bin/env bash
# Apply bootstrap treasury tuning on VPS (.env.settlement + script sync).
#
#   NODE_SSH=hackme-vps bash scripts/ops/apply_settlement_bootstrap_tuning.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

for f in ensure_settlement_treasury_float.sh treasury_bootstrap_guard.sh vps_settlement_autopilot.sh; do
  rsync -az "$ROOT/scripts/ops/$f" "$NODE_SSH:$DEPLOY/scripts/ops/"
done
rsync -az "$ROOT/scripts/ops/systemd/hackme-settlement-autopilot.service" \
  "$ROOT/scripts/ops/systemd/hackme-settlement-autopilot.timer" \
  "$NODE_SSH:/tmp/"

ssh -o BatchMode=yes "$NODE_SSH" bash -s <<EOF
set -euo pipefail
ENV="$DEPLOY/.env.settlement"
touch "\$ENV"
grep -v '^MIN_FLOAT_HMC=' "\$ENV" | grep -v '^TOPUP_HMC=' | grep -v '^MAX_GENESIS_TOPUP_24H_HMC=' >"\$ENV.tmp" || true
mv "\$ENV.tmp" "\$ENV"
cat >>"\$ENV" <<'ENVEOF'
MIN_FLOAT_HMC=15
TOPUP_HMC=20
MAX_GENESIS_TOPUP_24H_HMC=25
ENVEOF
chmod 600 "\$ENV"
sudo cp /tmp/hackme-settlement-autopilot.service /tmp/hackme-settlement-autopilot.timer /etc/systemd/system/ 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl enable --now hackme-settlement-autopilot.timer 2>/dev/null || true
echo "[apply-tuning] .env.settlement:"
grep -E 'MIN_FLOAT|TOPUP|MAX_GENESIS' "\$ENV" || true
EOF

NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$DEPLOY" bash "$ROOT/scripts/ops/vps_settlement_autopilot.sh"
echo "[apply-tuning] done"
