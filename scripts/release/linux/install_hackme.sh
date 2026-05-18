#!/usr/bin/env bash
set -euo pipefail

# Polished Linux installer for HackMe release.
#
# Usage:
#   sudo bash install_hackme.sh --archive ./hackme_1.0.0_linux.tar.gz
#
# Optional:
#   --install-dir /opt/hackme
#   --service-user hackme
#   --no-service

ARCHIVE=""
INSTALL_DIR="/opt/hackme"
SERVICE_USER="hackme"
ENABLE_SERVICE=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --archive) ARCHIVE="${2:-}"; shift 2 ;;
    --install-dir) INSTALL_DIR="${2:-}"; shift 2 ;;
    --service-user) SERVICE_USER="${2:-}"; shift 2 ;;
    --no-service) ENABLE_SERVICE=0; shift ;;
    *) echo "[install] unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "${ARCHIVE}" ]]; then
  echo "[install] --archive is required" >&2
  exit 2
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[install] missing command: $1" >&2
    exit 1
  }
}

require_cmd tar
require_cmd install
require_cmd sed

if [[ ! -f "${ARCHIVE}" ]]; then
  echo "[install] archive not found: ${ARCHIVE}" >&2
  exit 2
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

echo "[install] unpacking ${ARCHIVE}"
tar -xzf "${ARCHIVE}" -C "${TMP_DIR}"

PAYLOAD_DIR="$(find "${TMP_DIR}" -maxdepth 2 -type d -name linux | head -n 1)"
if [[ -z "${PAYLOAD_DIR}" ]]; then
  echo "[install] linux payload folder not found in archive" >&2
  exit 2
fi
if [[ ! -f "${PAYLOAD_DIR}/hackme" ]]; then
  echo "[install] hackme binary not found in payload" >&2
  exit 2
fi

if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
  echo "[install] creating system user: ${SERVICE_USER}"
  useradd --system --create-home --shell /usr/sbin/nologin "${SERVICE_USER}"
fi

echo "[install] installing to ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}"
install -m 0755 "${PAYLOAD_DIR}/hackme" "${INSTALL_DIR}/hackme"
install -m 0644 "${PAYLOAD_DIR}/README.md" "${INSTALL_DIR}/README.md" || true

if [[ ! -f "${INSTALL_DIR}/.env" ]]; then
  cat > "${INSTALL_DIR}/.env" <<EOF
HACKME_BIND_ADDR=127.0.0.1:8080
HACKME_REQUIRE_ADMIN_TOKEN=1
# HACKME_ADMIN_TOKEN=change_me
EOF
  chmod 0600 "${INSTALL_DIR}/.env"
fi

mkdir -p /usr/local/share/applications
if [[ -f "${PAYLOAD_DIR}/hackme.desktop.template" ]]; then
  sed "s#__INSTALL_DIR__#${INSTALL_DIR}#g" "${PAYLOAD_DIR}/hackme.desktop.template" > /usr/local/share/applications/hackme.desktop
fi

if [[ "${ENABLE_SERVICE}" == "1" ]]; then
  if [[ ! -f "${PAYLOAD_DIR}/hackme-node.service.template" ]]; then
    echo "[install] service template missing in payload" >&2
    exit 2
  fi
  sed \
    -e "s#__INSTALL_DIR__#${INSTALL_DIR}#g" \
    -e "s#__SERVICE_USER__#${SERVICE_USER}#g" \
    "${PAYLOAD_DIR}/hackme-node.service.template" > /etc/systemd/system/hackme-node.service
  systemctl daemon-reload
  systemctl enable --now hackme-node
fi

chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}"

if [[ -f "${PAYLOAD_DIR}/install_from_code_toolchains.sh" ]]; then
  echo "[install] from_code toolchains (zig, asc, tinygo, wat2wasm, rust)..."
  HACKME_PREFIX="${INSTALL_DIR}" bash "${PAYLOAD_DIR}/install_from_code_toolchains.sh" --system --prefix "${INSTALL_DIR}" || \
    echo "[install] WARN: toolchain install incomplete — see ${INSTALL_DIR}/.env.toolchains" >&2
  chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}" 2>/dev/null || true
fi

echo "[install] done"
echo "[install] dashboard:  http://127.0.0.1:8080/"
echo "[install] explorer:   http://127.0.0.1:8080/explorer"
