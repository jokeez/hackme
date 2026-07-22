#!/usr/bin/env bash
# Install / scale 3–10 workerfuzz units with unique WORKER_IDs + miner seeds.
# Intended for B2B bootstrap VPS (claims pool fuzz from hub coordinator).
#
#   WORKERFUZZ_COUNT=4 bash scripts/ops/bootstrap_customer/workerfuzz_fleet.sh install
#   bash scripts/ops/bootstrap_customer/workerfuzz_fleet.sh status
#   bash scripts/ops/bootstrap_customer/workerfuzz_fleet.sh stop
#
# Dig capacity for pool_distributed customer orders = # of distinct workerfuzz
# processes claiming /api/fuzz/work on the coordinator (GPU workerpoh does PoH only).
set -euo pipefail

INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
COUNT="${WORKERFUZZ_COUNT:-4}"
PREFIX="${WORKERFUZZ_ID_PREFIX:-bootstrap-fuzz}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
BIN="${WORKERFUZZ_BIN:-$INSTALL/bin/workerfuzz}"
ENV_DIR="${INSTALL}/.secrets/workerfuzz"
UNIT_PREFIX="hackme-bootstrap-workerfuzz"

if [[ "$COUNT" -lt 1 ]]; then COUNT=1; fi
if [[ "$COUNT" -gt 10 ]]; then
  echo "[workerfuzz-fleet] capping COUNT=10 (requested $COUNT)" >&2
  COUNT=10
fi

cmd="${1:-status}"

derive_seed() {
  local idx="$1" base="$2"
  python3 - "$idx" "$base" <<'PY'
import hashlib, sys
idx, base = sys.argv[1], sys.argv[2].strip().lower()
if len(base) != 64 or any(c not in "0123456789abcdef" for c in base):
    raise SystemExit(f"base seed must be 64 hex chars, got len={len(base)}")
h = hashlib.sha256(f"hackme-workerfuzz-fleet:{idx}:{base}".encode()).hexdigest()
print(h)
PY
}

load_coord_token() {
  if [[ -n "${COORD_TOKEN:-}" ]]; then
    printf '%s' "$COORD_TOKEN"
    return
  fi
  for f in \
    "$INSTALL/.secrets/coordinator_worker.token" \
    "$INSTALL/.secrets/hackme_coordinator_worker_token" \
    "$INSTALL/pool.miner.token"; do
    if [[ -f "$f" ]]; then
      tr -d '\r\n' <"$f"
      return
    fi
  done
  # Fallback: pool token from .env
  if [[ -f "$INSTALL/.env" ]]; then
    grep -m1 '^HACKME_POOL_COORDINATOR_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n' || true
  fi
}

load_base_seed() {
  if [[ -n "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
    printf '%s' "$HACKME_MINER_ED25519_SEED_HEX"
    return
  fi
  for f in \
    "$INSTALL/data/node_ed25519.seed" \
    "$INSTALL/.secrets/miner.seed.hex" \
    /opt/hackme-miner/.secrets/miner.seed.hex; do
    if [[ -f "$f" ]]; then
      tr -d '\r\n' <"$f"
      return
    fi
  done
  echo ""
}

install_fleet() {
  mkdir -p "$INSTALL/bin" "$ENV_DIR" "$INSTALL/logs/workerfuzz"
  chmod 700 "$ENV_DIR"

  if [[ ! -x "$BIN" ]]; then
    echo "[workerfuzz-fleet] missing binary $BIN — copy workerfuzz first" >&2
    exit 1
  fi

  local token seed
  token="$(load_coord_token)"
  seed="$(load_base_seed)"
  [[ -n "$token" ]] || { echo "[workerfuzz-fleet] missing COORD_TOKEN / pool token" >&2; exit 1; }
  [[ -n "$seed" ]] || { echo "[workerfuzz-fleet] missing miner seed" >&2; exit 1; }

  local i wid envf unit seed_i
  for i in $(seq 1 "$COUNT"); do
    wid=$(printf '%s-%02d' "$PREFIX" "$i")
    envf="$ENV_DIR/${wid}.env"
    seed_i="$(derive_seed "$i" "$seed")"
    umask 077
    cat >"$envf" <<EOF
COORD_URL=${COORD_URL}
COORD_TOKEN=${token}
WORKER_ID=${wid}
HACKME_MINER_ED25519_SEED_HEX=${seed_i}
WORKERFUZZ_HTTP_TIMEOUT_SEC=${WORKERFUZZ_HTTP_TIMEOUT_SEC:-120}
WORKERFUZZ_TIMEOUT_MS=${WORKERFUZZ_TIMEOUT_MS:-800}
EOF
    chmod 600 "$envf"

    unit="/etc/systemd/system/${UNIT_PREFIX}@${wid}.service"
    cat >"$unit" <<EOF
[Unit]
Description=HackMe bootstrap workerfuzz %i
After=network-online.target hackme-bootstrap-node.service
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${INSTALL}
EnvironmentFile=${envf}
Environment=BOOTSTRAP_INSTALL=${INSTALL}
Environment=WORKERFUZZ_BIN=${BIN}
ExecStart=/bin/bash ${INSTALL}/scripts/bootstrap_customer/workerfuzz_instance.sh
Restart=always
RestartSec=8
Nice=10
CPUQuota=40%
MemoryMax=512M

[Install]
WantedBy=multi-user.target
EOF
    systemctl enable "${UNIT_PREFIX}@${wid}.service"
    systemctl restart "${UNIT_PREFIX}@${wid}.service"
    echo "[workerfuzz-fleet] started ${wid}"
  done
  systemctl daemon-reload
  echo "[workerfuzz-fleet] install ok count=$COUNT prefix=$PREFIX"
}

status_fleet() {
  systemctl list-units "${UNIT_PREFIX}@*" --no-pager 2>/dev/null || true
  local i wid
  for i in $(seq 1 10); do
    wid=$(printf '%s-%02d' "$PREFIX" "$i")
    if systemctl cat "${UNIT_PREFIX}@${wid}.service" >/dev/null 2>&1; then
      printf '%s ' "$wid"
      systemctl is-active "${UNIT_PREFIX}@${wid}.service" 2>/dev/null || echo inactive
    fi
  done
}

stop_fleet() {
  local i wid
  for i in $(seq 1 10); do
    wid=$(printf '%s-%02d' "$PREFIX" "$i")
    if systemctl cat "${UNIT_PREFIX}@${wid}.service" >/dev/null 2>&1; then
      systemctl stop "${UNIT_PREFIX}@${wid}.service" 2>/dev/null || true
      systemctl disable "${UNIT_PREFIX}@${wid}.service" 2>/dev/null || true
      echo "[workerfuzz-fleet] stopped ${wid}"
    fi
  done
}

case "$cmd" in
  install|start) install_fleet ;;
  status) status_fleet ;;
  stop) stop_fleet ;;
  *)
    echo "usage: $0 install|status|stop" >&2
    exit 2
    ;;
esac
