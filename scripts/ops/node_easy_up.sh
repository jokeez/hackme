#!/usr/bin/env bash
set -euo pipefail

# Easy node bring-up for leader/follower modes.
# Reduces manual env/token sprawl for private network tests.
#
# Usage examples:
#   TOKEN_SECRET="my-secret" ROLE=leader ADVERTISE_URL="http://192.168.1.113:8080" bash scripts/ops/node_easy_up.sh
#   TOKEN_SECRET="my-secret" ROLE=follower ADVERTISE_URL="http://192.168.1.133:8080" PEERS="http://192.168.1.113:8080" bash scripts/ops/node_easy_up.sh
#
# Required:
#   ROLE            leader | follower
#   ADVERTISE_URL   public URL of this node (http://IP:8080)
#
# Token options (choose one):
#   ADMIN_TOKEN + P2P_TOKEN    explicit tokens
#   TOKEN_SECRET               derives both tokens deterministically
#
# Optional:
#   BIND_ADDR      default 0.0.0.0:8080
#   PEERS          comma-separated peer URLs (default empty)
#   SANDBOX_PROFILE default secure
#   FUZZ_AUTORUN    default 1
#   ENABLE_MINING   default 1 for leader, 0 for follower
#   LOG_FILE        default logs/node_<role>_<ts>.log

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[node-easy-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd go
require_cmd curl
require_cmd jq
require_cmd sha256sum
require_cmd ss

ROLE="${ROLE:-}"
ADVERTISE_URL="${ADVERTISE_URL:-}"
BIND_ADDR="${BIND_ADDR:-0.0.0.0:8080}"
PEERS="${PEERS:-}"
LEADER_URL="${LEADER_URL:-}"
SANDBOX_PROFILE="${SANDBOX_PROFILE:-secure}"
FUZZ_AUTORUN="${FUZZ_AUTORUN:-1}"
TOKEN_SECRET="${TOKEN_SECRET:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
COORD_URL="${COORD_URL:-${HACKME_POOL_COORDINATOR_URL:-}}"
COORD_TOKEN="${COORD_TOKEN:-${COORD_ADMIN_TOKEN:-${HACKME_POOL_COORDINATOR_TOKEN:-}}}"

if [[ -z "$ROLE" || ( "$ROLE" != "leader" && "$ROLE" != "follower" ) ]]; then
  echo "[node-easy-up] ROLE must be leader|follower" >&2
  exit 1
fi
if [[ -z "$ADVERTISE_URL" ]]; then
  echo "[node-easy-up] ADVERTISE_URL is required (e.g. http://192.168.1.113:8080)" >&2
  exit 1
fi
if [[ "$ADVERTISE_URL" != http://* && "$ADVERTISE_URL" != https://* ]]; then
  echo "[node-easy-up] ADVERTISE_URL must start with http:// or https://" >&2
  exit 1
fi

if [[ -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" ]]; then
  if [[ -z "$TOKEN_SECRET" ]]; then
    echo "[node-easy-up] provide ADMIN_TOKEN+P2P_TOKEN or TOKEN_SECRET" >&2
    exit 1
  fi
  # Deterministic derivation for private-network convenience.
  ADMIN_TOKEN="HMC_ADMIN_$(printf '%s' "admin|$TOKEN_SECRET" | sha256sum | awk '{print $1}' | cut -c1-32)"
  P2P_TOKEN="$(printf '%s' "p2p|$TOKEN_SECRET" | sha256sum | awk '{print $1}' | cut -c1-48)"
fi

# Convenience alias: many operators set LEADER_URL for follower mode.
if [[ -z "$PEERS" && -n "$LEADER_URL" ]]; then
  PEERS="$LEADER_URL"
fi

ENABLE_MINING="${ENABLE_MINING:-}"
if [[ -z "$ENABLE_MINING" ]]; then
  if [[ "$ROLE" == "leader" ]]; then
    ENABLE_MINING="1"
  else
    ENABLE_MINING="0"
  fi
fi

if [[ "$ROLE" == "follower" && -z "$PEERS" ]]; then
  echo "[node-easy-up] follower mode requires PEERS or LEADER_URL" >&2
  exit 1
fi

if [[ -z "$COORD_URL" && -n "$PEERS" ]]; then
  first_peer="${PEERS%%,*}"
  first_peer="$(printf '%s' "$first_peer" | xargs)"
  if [[ "$first_peer" =~ ^https?:// ]]; then
    scheme="${first_peer%%://*}"
    hostport_path="${first_peer#*://}"
    hostport="${hostport_path%%/*}"
    if [[ "$hostport" == *:* ]]; then
      host="${hostport%:*}"
      port="${hostport##*:}"
      if [[ "$port" =~ ^[0-9]+$ ]]; then
        COORD_URL="${scheme}://${host}:$((port + 1))"
      fi
    fi
  fi
fi
if [[ -z "$COORD_TOKEN" ]]; then
  COORD_TOKEN="$ADMIN_TOKEN"
fi

mkdir -p logs
ts="$(date -u +%Y%m%dT%H%M%SZ)"
LOG_FILE="${LOG_FILE:-$ROOT_DIR/logs/node_${ROLE}_${ts}.log}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:${BIND_ADDR##*:}}"
bind_port="${BIND_ADDR##*:}"

echo "[node-easy-up] role=$ROLE bind=$BIND_ADDR advertise=$ADVERTISE_URL"
echo "[node-easy-up] peers=${PEERS:-<none>}"
echo "[node-easy-up] coordinator=${COORD_URL:-<auto-disabled>} token_set=$([[ -n "$COORD_TOKEN" ]] && echo 1 || echo 0)"
echo "[node-easy-up] log=$LOG_FILE"

if ss -ltnp 2>/dev/null | awk -v p=":${bind_port}" '$4 ~ (p "$") {found=1} END{exit (found?0:1)}'; then
  echo "[node-easy-up] ERROR: bind port ${bind_port} already in use. Stop old process first." >&2
  ss -ltnp 2>/dev/null | awk -v p=":${bind_port}" '$4 ~ (p "$") {print}'
  exit 1
fi

# POST /api/mining/start is allowed only when HACKME_CHAIN_LEADER_LOCAL_POH=1 (chain command node).
# When ENABLE_MINING=1 we default that on for this bring-up helper (override with explicit HACKME_CHAIN_LEADER_LOCAL_POH=0).
if [[ -n "${HACKME_CHAIN_LEADER_LOCAL_POH+x}" ]]; then
  CHAIN_LEADER_POH_VAL="$HACKME_CHAIN_LEADER_LOCAL_POH"
elif [[ "$ENABLE_MINING" == "1" ]]; then
  CHAIN_LEADER_POH_VAL="1"
else
  CHAIN_LEADER_POH_VAL="0"
fi

# Follower / pool participant defaults: dashboard blend + hybrid submits use node_ed25519.seed (same HMC- as Node ID).
if [[ "$ROLE" == "follower" ]]; then
	if [[ -z "${HACKME_DESKTOP_MODE:-}" ]]; then
		HACKME_DESKTOP_MODE=1
	fi
	if [[ -z "${HACKME_UNIFIED_MINER_NODE_SEED:-}" ]]; then
		HACKME_UNIFIED_MINER_NODE_SEED=1
	fi
fi

nohup env \
  HACKME_BIND_ADDR="$BIND_ADDR" \
  HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_P2P_TOKEN="$P2P_TOKEN" \
  HACKME_P2P_DISCOVERY="1" \
  HACKME_P2P_ADVERTISE_URL="$ADVERTISE_URL" \
  HACKME_P2P_PEERS="$PEERS" \
  HACKME_P2P_SYNC_STATE_REPLAY_ENABLED="$([[ "$ROLE" == "follower" ]] && echo 1 || echo 0)" \
  HACKME_POOL_COORDINATOR_URL="$COORD_URL" \
  HACKME_POOL_COORDINATOR_TOKEN="$COORD_TOKEN" \
  HACKME_FUZZ_AUTORUN="$FUZZ_AUTORUN" \
  HACKME_SANDBOX_PROFILE="$SANDBOX_PROFILE" \
  HACKME_CHAIN_LEADER_LOCAL_POH="$CHAIN_LEADER_POH_VAL" \
  HACKME_DESKTOP_MODE="${HACKME_DESKTOP_MODE:-}" \
  HACKME_UNIFIED_MINER_NODE_SEED="${HACKME_UNIFIED_MINER_NODE_SEED:-}" \
  go run . >"$LOG_FILE" 2>&1 &
pid=$!
echo "[node-easy-up] started pid=$pid"

sleep 2
if ! curl -fsS "${LOCAL_BASE}/api/status" >/dev/null 2>&1; then
  echo "[node-easy-up] WARN: local /api/status not reachable yet, check log: $LOG_FILE" >&2
else
  curl -sS "${LOCAL_BASE}/api/status" | jq '{has_genesis,tip_height,tip_hash,node_address,mining}'
fi

if [[ "$ENABLE_MINING" == "1" ]]; then
  echo "[node-easy-up] attempting mining start"
  curl -sS -X POST "${LOCAL_BASE}/api/mining/start" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" | jq . || true
fi

echo
echo "[node-easy-up] token export for this shell:"
echo "export HACKME_ADMIN_TOKEN=\"$ADMIN_TOKEN\""
echo "export HACKME_P2P_TOKEN=\"$P2P_TOKEN\""
echo
echo "[node-easy-up] follower autopilot (optional):"
echo "BASE=\"${LOCAL_BASE}\" ADMIN_TOKEN=\"$ADMIN_TOKEN\" bash scripts/ops/p2p_autopilot.sh"
