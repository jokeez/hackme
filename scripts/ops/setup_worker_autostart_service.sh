#!/usr/bin/env bash
set -euo pipefail

# One-click setup for workerpoh autostart service.
#
# Usage:
#   sudo bash scripts/ops/setup_worker_autostart_service.sh
#
# Optional env:
#   HACKME_ROOT=/opt/hackme
#   ENV_FILE=/opt/hackme/.env.worker
#   INSTALL_DEPS=1

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "[worker-service-setup] run as root (use sudo)" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR_DEFAULT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ROOT_DIR="${HACKME_ROOT:-$ROOT_DIR_DEFAULT}"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/.env.worker}"
INSTALL_DEPS="${INSTALL_DEPS:-1}"

service_user="${SUDO_USER:-}"
if [[ -z "$service_user" || "$service_user" == "root" ]]; then
  # fallback when script is run by direct root login
  service_user="$(stat -c '%U' "$ROOT_DIR" 2>/dev/null || true)"
fi
if [[ -z "$service_user" || "$service_user" == "root" ]]; then
  echo "[worker-service-setup] cannot infer non-root service user; set ownership of ${ROOT_DIR} to your user first" >&2
  exit 1
fi

echo "[worker-service-setup] root=${ROOT_DIR} user=${service_user}"

if [[ "$INSTALL_DEPS" == "1" ]]; then
  if command -v apt-get >/dev/null 2>&1; then
    echo "[worker-service-setup] installing runtime dependencies (best effort)"
    apt-get update -y || true
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
      ca-certificates curl jq build-essential pkg-config \
      ocl-icd-libopencl1 ocl-icd-opencl-dev opencl-headers clinfo || true
  fi
fi

install -d -m 0755 "${ROOT_DIR}/logs" "${ROOT_DIR}/bin"
chown -R "${service_user}:${service_user}" "${ROOT_DIR}/logs" "${ROOT_DIR}/bin"

if [[ ! -f "$ENV_FILE" ]]; then
  cp "${ROOT_DIR}/scripts/ops/worker.env.example" "$ENV_FILE"
  chown "${service_user}:${service_user}" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  echo "[worker-service-setup] created ${ENV_FILE} from example"
fi

chmod +x "${ROOT_DIR}/scripts/ops/worker_autostart.sh"

unit_path="/etc/systemd/system/hackme-workerpoh.service"
cat >"$unit_path" <<EOF
[Unit]
Description=HackMe PoH Worker Autostart
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${service_user}
Group=${service_user}
WorkingDirectory=${ROOT_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${ROOT_DIR}/scripts/ops/worker_autostart.sh
Restart=always
RestartSec=3
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=read-only
ReadWritePaths=${ROOT_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now hackme-workerpoh.service

echo "[worker-service-setup] service enabled"
systemctl --no-pager --full status hackme-workerpoh.service || true

echo
echo "Next steps:"
echo "  1) Edit ${ENV_FILE} (COORD_URL, COORD_TOKEN, HACKME_MINER_ED25519_SEED_HEX)."
echo "  2) Restart service: sudo systemctl restart hackme-workerpoh.service"
echo "  3) Logs: journalctl -u hackme-workerpoh.service -f"
