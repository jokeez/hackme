#!/usr/bin/env bash
# One-time setup for the Linux release bundle (no Go toolchain required).
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$INSTALL_DIR"

ENV_FILE="${ENV_FILE:-$INSTALL_DIR/.env}"
POOL_FILE="$INSTALL_DIR/pool.miner.token"

if [[ ! -x "$INSTALL_DIR/hackme" ]]; then
  echo "[setup] hackme binary missing in $INSTALL_DIR" >&2
  exit 1
fi
if [[ ! -f "$POOL_FILE" ]]; then
  echo "[setup] pool.miner.token missing — download a fresh bundle from https://hackme.tech/downloads.html" >&2
  exit 1
fi

POOL_TOKEN="$(tr -d '\r\n' <"$POOL_FILE")"
if [[ -z "$POOL_TOKEN" || "$POOL_TOKEN" == "REPLACE_WITH_POOL_TOKEN" ]]; then
  echo "[setup] pool.miner.token is empty or placeholder" >&2
  exit 1
fi

admin=""
if [[ -f "$ENV_FILE" ]]; then
  admin="$(grep -E '^HACKME_ADMIN_TOKEN=' "$ENV_FILE" | head -n1 | cut -d= -f2- | tr -d '\r' || true)"
fi
if [[ -z "$admin" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    admin="$(openssl rand -hex 24)"
  else
    admin="$(python3 -c 'import secrets; print(secrets.token_hex(24))')"
  fi
fi

export HACKME_REPO_ROOT="$INSTALL_DIR"
GPU_BACKEND="auto"
if [[ -x "$INSTALL_DIR/detect_gpu_backend.sh" ]]; then
  GPU_BACKEND="$(bash "$INSTALL_DIR/detect_gpu_backend.sh" 2>/dev/null || echo auto)"
fi
if [[ -z "$GPU_BACKEND" ]]; then
  GPU_BACKEND="auto"
fi

mkdir -p "$INSTALL_DIR/logs" "$INSTALL_DIR/data"
chmod 700 "$INSTALL_DIR/data" 2>/dev/null || true

cat >"$ENV_FILE" <<EOF
HACKME_BIND_ADDR=127.0.0.1:8080
HACKME_ADMIN_TOKEN=${admin}
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_DESKTOP_MODE=1
HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech
HACKME_CANONICAL_CHAIN_URL=https://hackme.tech
HACKME_POOL_COORDINATOR_TOKEN=${POOL_TOKEN}
HACKME_GPU_BACKEND=${GPU_BACKEND}
HACKME_WORKER_WATCHDOG=1
HACKME_DATA_DIR=${INSTALL_DIR}/data
DESKTOP_PROFILE=worker
EOF
chmod 600 "$ENV_FILE" 2>/dev/null || true

echo "[setup] wrote $ENV_FILE (GPU backend: ${GPU_BACKEND})"
echo "[setup] run: bash start_hackme_miner.sh"
