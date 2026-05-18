#!/usr/bin/env bash
set -euo pipefail

# Install worker settlement service/timers on VPS.
#
# Usage:
#   sudo bash scripts/ops/setup_worker_settlement_service.sh

ROOT="/opt/hackme"
ENV_FILE="${ROOT}/.env.settlement"

units=(
  "hackme-worker-settlement.service"
  "hackme-worker-settlement.timer"
  "hackme-worker-settlement-healthcheck.service"
  "hackme-worker-settlement-healthcheck.timer"
)

for u in "${units[@]}"; do
  src="${ROOT}/scripts/ops/systemd/${u}"
  if [[ ! -f "$src" ]]; then
    echo "[settlement-setup] missing unit: $src" >&2
    exit 1
  fi
  install -m 0644 "$src" "/etc/systemd/system/${u}"
done

mkdir -p "${ROOT}/data" "${ROOT}/logs"
chown -R hackme:hackme "${ROOT}/data" "${ROOT}/logs"
touch "${ROOT}/data/worker_settlement_state.json"
chown hackme:hackme "${ROOT}/data/worker_settlement_state.json"
chmod 600 "${ROOT}/data/worker_settlement_state.json"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "${ROOT}/scripts/ops/settlement.env.example" "$ENV_FILE"
  chown hackme:hackme "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  echo "[settlement-setup] created ${ENV_FILE} from example; edit ADMIN_TOKEN and optional WORKER_PAYOUT_MAP first"
fi

chmod +x "${ROOT}/scripts/ops/settle_worker_payouts.sh"
chmod +x "${ROOT}/scripts/ops/settlement_healthcheck.sh"

systemctl daemon-reload
systemctl enable --now hackme-worker-settlement.timer
systemctl enable --now hackme-worker-settlement-healthcheck.timer

echo "[settlement-setup] timers enabled"
systemctl --no-pager --full status hackme-worker-settlement.timer || true
systemctl --no-pager --full status hackme-worker-settlement-healthcheck.timer || true

echo
echo "Manual checks:"
echo "  systemctl list-timers --all | rg 'hackme-worker-settlement'"
echo "  journalctl -u hackme-worker-settlement.service -n 80 --no-pager"
echo "  journalctl -u hackme-worker-settlement-healthcheck.service -n 80 --no-pager"
