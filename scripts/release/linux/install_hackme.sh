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
mkdir -p "${INSTALL_DIR}/bin" "${INSTALL_DIR}/scripts/ops" "${INSTALL_DIR}/data" "${INSTALL_DIR}/logs"
install -m 0755 "${PAYLOAD_DIR}/hackme" "${INSTALL_DIR}/hackme"
install -m 0644 "${PAYLOAD_DIR}/README.md" "${INSTALL_DIR}/README.md" || true
for b in workerpoh workerfuzz minersign fleetplan workerpoh-opencl workerpoh-cuda workerpoh-cpu hackme-wasm-check; do
  if [[ -f "${PAYLOAD_DIR}/${b}" ]]; then
    install -m 0755 "${PAYLOAD_DIR}/${b}" "${INSTALL_DIR}/${b}"
  fi
  if [[ -f "${PAYLOAD_DIR}/bin/${b}" ]]; then
    install -m 0755 "${PAYLOAD_DIR}/bin/${b}" "${INSTALL_DIR}/bin/${b}"
  fi
done
[[ -d "${PAYLOAD_DIR}/bin" ]] && cp -a "${PAYLOAD_DIR}/bin/." "${INSTALL_DIR}/bin/" 2>/dev/null || true
[[ -d "${PAYLOAD_DIR}/lib" ]] && cp -a "${PAYLOAD_DIR}/lib" "${INSTALL_DIR}/lib"
for helper in update_hackme_miner.sh update_hackme_os_binaries.sh start_hackme_miner.sh stop_hackme_miner.sh \
              install_hunt_engines.sh prune_hunt_miner_caches.sh install_from_code_toolchains.sh \
              install_menu_entry.sh setup_hackme_miner.sh hackme_desktop_launch.sh \
              detect_gpu_backend.sh fix_miner_layout.sh; do
  if [[ -f "${PAYLOAD_DIR}/${helper}" ]]; then
    install -m 0755 "${PAYLOAD_DIR}/${helper}" "${INSTALL_DIR}/${helper}"
  fi
done
for op in worker_autostart.sh worker_loop.sh stop_pool_workers.sh resume_pool_mining.sh \
          purge_stale_pool_workers.sh detect_gpu_backend.sh desktop_worker_reset.sh; do
  if [[ -f "${PAYLOAD_DIR}/scripts/ops/${op}" ]]; then
    install -m 0755 "${PAYLOAD_DIR}/scripts/ops/${op}" "${INSTALL_DIR}/scripts/ops/${op}"
    install -m 0755 "${PAYLOAD_DIR}/scripts/ops/${op}" "${INSTALL_DIR}/${op}"
  elif [[ -f "${PAYLOAD_DIR}/${op}" ]]; then
    install -m 0755 "${PAYLOAD_DIR}/${op}" "${INSTALL_DIR}/${op}"
    install -m 0755 "${PAYLOAD_DIR}/${op}" "${INSTALL_DIR}/scripts/ops/${op}"
  fi
done
if [[ -d "${PAYLOAD_DIR}/icons" ]]; then
  mkdir -p "${INSTALL_DIR}/icons"
  cp -a "${PAYLOAD_DIR}/icons/." "${INSTALL_DIR}/icons/"
fi
for f in hackme.png hackme.desktop.template hackme-dashboard.desktop.template; do
  [[ -f "${PAYLOAD_DIR}/${f}" ]] && install -m 0644 "${PAYLOAD_DIR}/${f}" "${INSTALL_DIR}/${f}"
done
# Public pool worker token (not admin). Required by start_hackme_miner.sh.
if [[ -f "${PAYLOAD_DIR}/pool.miner.token" ]]; then
  install -m 0640 "${PAYLOAD_DIR}/pool.miner.token" "${INSTALL_DIR}/pool.miner.token"
fi
# Example only — never install a real pool.orders.token from the public bundle.
if [[ -f "${PAYLOAD_DIR}/pool.orders.token.example" ]]; then
  install -m 0644 "${PAYLOAD_DIR}/pool.orders.token.example" "${INSTALL_DIR}/pool.orders.token.example"
fi

if [[ ! -f "${INSTALL_DIR}/.env" ]]; then
  admin=""
  if command -v openssl >/dev/null 2>&1; then
    admin="$(openssl rand -hex 24)"
  else
    admin="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  fi
  cat > "${INSTALL_DIR}/.env" <<EOF
HACKME_BIND_ADDR=127.0.0.1:8080
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_ADMIN_TOKEN=${admin}
HACKME_DESKTOP_MODE=1
HACKME_INTEGRATOR_SELF_REGISTER=1
HACKME_WORKER_WATCHDOG=1
WORKER_AUTOSTART=1
HACKME_WORKER_LOCK_DIR=${INSTALL_DIR}/logs
HACKME_WORKER_HEARTBEAT_STALE_SEC=180
HACKME_WORKER_HEARTBEAT_GRACE_SEC=120
HACKME_FUZZ_SETTLE_PULL=0
HACKME_DATA_DIR=${INSTALL_DIR}/data
EOF
  chmod 0600 "${INSTALL_DIR}/.env"
  echo "[install] generated HACKME_ADMIN_TOKEN in ${INSTALL_DIR}/.env"
fi

# Branded application menu entry (HackMe logo)
if [[ -x "${INSTALL_DIR}/install_menu_entry.sh" ]]; then
  INSTALL_DIR="${INSTALL_DIR}" PAYLOAD_DIR="${PAYLOAD_DIR}" \
    bash "${INSTALL_DIR}/install_menu_entry.sh" --install-dir "${INSTALL_DIR}" --payload-dir "${PAYLOAD_DIR}" || true
elif [[ -f "${PAYLOAD_DIR}/hackme.desktop.template" ]]; then
  mkdir -p /usr/local/share/applications
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

# Immutable install tree: root owns binaries/scripts; service user data/logs/.env/.cache.
chown -R root:root "${INSTALL_DIR}"
chmod -R go-w "${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}/data" "${INSTALL_DIR}/logs" "${INSTALL_DIR}/.cache"
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}/data" "${INSTALL_DIR}/logs" "${INSTALL_DIR}/.cache"
[[ -f "${INSTALL_DIR}/.env" ]] && chown "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}/.env" && chmod 0600 "${INSTALL_DIR}/.env"
[[ -f "${INSTALL_DIR}/pool.miner.token" ]] && chown root:"${SERVICE_USER}" "${INSTALL_DIR}/pool.miner.token" && chmod 0640 "${INSTALL_DIR}/pool.miner.token"

if [[ -f "${PAYLOAD_DIR}/install_from_code_toolchains.sh" ]]; then
  echo "[install] from_code toolchains (zig, asc, tinygo, wat2wasm, rust)..."
  HACKME_PREFIX="${INSTALL_DIR}" bash "${PAYLOAD_DIR}/install_from_code_toolchains.sh" --system --prefix "${INSTALL_DIR}" || \
    echo "[install] WARN: toolchain install incomplete — see ${INSTALL_DIR}/.env.toolchains" >&2
  # Toolchains under install dir stay root-owned (read-only for service user).
  chown -R root:root "${INSTALL_DIR}/toolchains" 2>/dev/null || true
fi

if [[ "${HACKME_SKIP_HUNT_ENGINES:-0}" != "1" ]]; then
  eng=""
  for cand in "${INSTALL_DIR}/install_hunt_engines.sh" "${PAYLOAD_DIR}/install_hunt_engines.sh"; do
    [[ -f "$cand" ]] && { eng="$cand"; break; }
  done
  if [[ -n "$eng" ]]; then
    echo "[install] Hunt engines (clang, AFL++, bubblewrap)..."
    HACKME_PREFIX="${INSTALL_DIR}" bash "$eng" --system --prefix "${INSTALL_DIR}" || \
      echo "[install] WARN: Hunt engines incomplete — Hunt soft-skips; Dig unchanged" >&2
    # Hunt writes harness/AFL/seed caches under .cache; keep service-user owned.
    # Toolchains stay root-owned above.
    mkdir -p "${INSTALL_DIR}/.cache"
    chown -R "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}/.cache" 2>/dev/null || true
  fi
fi

echo "[install] done"
echo "[install] dashboard:  http://127.0.0.1:8080/"
echo "[install] explorer:   http://127.0.0.1:8080/explorer"
echo "[install] menu:       HackMe / HackMe Dashboard (app launcher)"
echo "[install] updates:    bash ${INSTALL_DIR}/update_hackme_miner.sh  (keeps .env/data/logs)"
if [[ -x "${INSTALL_DIR}/update_hackme_miner.sh" ]]; then
  :
elif [[ -f "${PAYLOAD_DIR}/update_hackme_miner.sh" ]]; then
  install -m 0755 "${PAYLOAD_DIR}/update_hackme_miner.sh" "${INSTALL_DIR}/update_hackme_miner.sh"
  chown "${SERVICE_USER}:${SERVICE_USER}" "${INSTALL_DIR}/update_hackme_miner.sh" 2>/dev/null || true
fi
