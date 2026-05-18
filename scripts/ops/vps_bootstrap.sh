#!/usr/bin/env bash
set -euo pipefail

# Prepares VPS runtime env and prints deploy commands.
# Does not require root; only writes local .env file.
#
# Usage:
#   PUBLIC_HOST=1.2.3.4 ROLE=leader bash scripts/ops/vps_bootstrap.sh
#   PUBLIC_HOST=node.example.com ROLE=follower PEERS=http://1.2.3.4:8080 bash scripts/ops/vps_bootstrap.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[vps-bootstrap] missing command: $1" >&2
    exit 1
  }
}

require_cmd sha256sum

ROLE="${ROLE:-leader}"
PUBLIC_HOST="${PUBLIC_HOST:-}"
PORT="${PORT:-8080}"
PEERS="${PEERS:-}"
TOKEN_SECRET="${TOKEN_SECRET:-}"
ENV_PATH="${ENV_PATH:-$ROOT_DIR/.env.vps}"
P2P_DISCOVERY="${P2P_DISCOVERY:-1}"
FUZZ_AUTORUN="${FUZZ_AUTORUN:-1}"
SANDBOX_PROFILE="${SANDBOX_PROFILE:-secure}"

if [[ "$ROLE" != "leader" && "$ROLE" != "follower" ]]; then
  echo "[vps-bootstrap] ROLE must be leader|follower" >&2
  exit 1
fi
if [[ -z "$PUBLIC_HOST" ]]; then
  echo "[vps-bootstrap] PUBLIC_HOST is required (IP or DNS name)" >&2
  exit 1
fi
if [[ -z "$TOKEN_SECRET" ]]; then
  TOKEN_SECRET="$(date -u +%s)-$(hostname)-$RANDOM-$RANDOM"
fi

ADMIN_TOKEN="HMC_ADMIN_$(printf '%s' "admin|$TOKEN_SECRET" | sha256sum | awk '{print $1}' | cut -c1-32)"
P2P_TOKEN="$(printf '%s' "p2p|$TOKEN_SECRET" | sha256sum | awk '{print $1}' | cut -c1-48)"
ADVERTISE_URL="http://${PUBLIC_HOST}:${PORT}"

cat >"$ENV_PATH" <<EOF
HACKME_BIND_ADDR=0.0.0.0:${PORT}
HACKME_ADMIN_TOKEN=${ADMIN_TOKEN}
HACKME_P2P_TOKEN=${P2P_TOKEN}
HACKME_P2P_DISCOVERY=${P2P_DISCOVERY}
HACKME_P2P_ADVERTISE_URL=${ADVERTISE_URL}
HACKME_P2P_PEERS=${PEERS}
HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=$([[ "$ROLE" == "follower" ]] && echo 1 || echo 0)
HACKME_FUZZ_AUTORUN=${FUZZ_AUTORUN}
HACKME_SANDBOX_PROFILE=${SANDBOX_PROFILE}
EOF

chmod 600 "$ENV_PATH"

echo "[vps-bootstrap] wrote $ENV_PATH"
echo "[vps-bootstrap] role=$ROLE advertise=$ADVERTISE_URL peers=${PEERS:-<none>}"
echo
echo "Next (run on VPS as root/sudo where needed):"
echo "1) Create service user:"
echo "   sudo useradd --system --create-home --shell /usr/sbin/nologin hackme || true"
echo "2) Copy project to /opt/hackme and env:"
echo "   sudo mkdir -p /opt/hackme && sudo rsync -a ./ /opt/hackme/"
echo "   sudo cp \"$ENV_PATH\" /opt/hackme/.env.vps && sudo chown -R hackme:hackme /opt/hackme"
echo "3) Install systemd unit:"
echo "   sudo cp scripts/ops/systemd/hackme-node.service /etc/systemd/system/hackme-node.service"
echo "   sudo systemctl daemon-reload && sudo systemctl enable --now hackme-node"
echo "4) Firewall:"
echo "   sudo ufw allow ${PORT}/tcp"
echo "   sudo ufw allow 22/tcp"
echo "5) Optional — компиляторы для POST /api/tasks/from_code (Zig + asc + базовые пакеты):"
echo "   sudo bash /opt/hackme/scripts/ops/install_vps_from_code_toolchains.sh"
echo "   sudo systemctl daemon-reload && sudo systemctl restart hackme-node"
echo "6) Validate:"
echo "   curl -fsS http://127.0.0.1:${PORT}/api/status | jq '{tip_height,node_address,mining,has_genesis}'"
echo "   ADMIN_TOKEN=${ADMIN_TOKEN} BASE=http://127.0.0.1:${PORT} bash scripts/ops/fuzz_super_gate.sh"
