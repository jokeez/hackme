#!/usr/bin/env bash
# Reseed desktop SQLite from canonical VPS leader, then restart + incremental P2P sync.
#
# Use when local tip lags canonical (fork or stale follower) and :18080 P2P is unreachable.
#
#   bash scripts/ops/sync_desktop_canonical_db.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/ops/_deploy_ssh_retry.sh
source "$ROOT/scripts/ops/_deploy_ssh_retry.sh"

VPS_SSH="${VPS_SSH:-root@132.243.112.100}"
VPS_DATA="${VPS_DATA:-/opt/hackme/data/hackme.db}"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
ADMIN_FILE="${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}"
LOCAL_DB="${HACKME_DATA_DIR:-$ROOT/data}/hackme.db"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP="${LOCAL_DB}.pre-reseed-${STAMP}"

log() { echo "[desktop-sync] $*"; }

[[ -f "$DESKTOP_ENV" ]] && set -a && . "$DESKTOP_ENV" && set +a
LOCAL_DB="${HACKME_DATA_DIR:-$ROOT/data}/hackme.db"
ADMIN="$(tr -d '\r\n' <"$ADMIN_FILE" 2>/dev/null || true)"
[[ -n "$ADMIN" ]] || { echo "need admin token in $ADMIN_FILE" >&2; exit 2; }

_deploy_ssh() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    ssh -i "${HACKME_DEPLOY_SSH_IDENTITY}" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$@"
  else
    ssh "$@"
  fi
}
_deploy_rsync() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    rsync -e "ssh -i ${HACKME_DEPLOY_SSH_IDENTITY} -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new" "$@"
  else
    rsync "$@"
  fi
}

log "stop desktop node + worker"
pkill -f 'logs/desktop/hackme-node-desktop' 2>/dev/null || true
pkill -f 'bin/workerpoh' 2>/dev/null || true
sleep 2
if ss -tlnp 2>/dev/null | grep -q '127.0.0.1:8080'; then
  stale="$(ss -tlnp 2>/dev/null | grep '127.0.0.1:8080' | sed -n 's/.*pid=\([0-9]*\).*/\1/p' | head -1 || true)"
  [[ -n "$stale" ]] && kill "$stale" 2>/dev/null || true
  sleep 1
fi

mkdir -p "$(dirname "$LOCAL_DB")"
if [[ -f "$LOCAL_DB" ]]; then
  log "backup local db → $BACKUP"
  cp -a "$LOCAL_DB" "$BACKUP"
fi

log "rsync canonical db from $VPS_SSH:$VPS_DATA"
_deploy_rsync -az --progress "${VPS_SSH}:${VPS_DATA}" "$LOCAL_DB"
chmod 600 "$LOCAL_DB"

# Prefer HTTPS canonical peer (18080 often closed on VPS firewall).
if grep -q '^HACKME_P2P_PEERS=' "$DESKTOP_ENV" 2>/dev/null; then
  sed -i 's|^HACKME_P2P_PEERS=.*|HACKME_P2P_PEERS=https://hackme.tech/pool|' "$DESKTOP_ENV"
else
  echo 'HACKME_P2P_PEERS=https://hackme.tech/pool' >>"$DESKTOP_ENV"
fi
grep -q '^HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=' "$DESKTOP_ENV" 2>/dev/null || \
  echo 'HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1' >>"$DESKTOP_ENV"
grep -q '^HACKME_P2P_BACKGROUND_SYNC_SEC=' "$DESKTOP_ENV" 2>/dev/null || \
  echo 'HACKME_P2P_BACKGROUND_SYNC_SEC=30' >>"$DESKTOP_ENV"

log "restart desktop stack"
bash "$ROOT/scripts/ops/restart_linux_desktop_worker.sh"

sleep 5
LOCAL_TIP="$(curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" 'http://127.0.0.1:8080/api/status?lite=1' | jq -r '.tip_height // 0')"
CANON_TIP="$(curl -fsS --max-time 15 'https://hackme.tech/pool/api/status?lite=1' | jq -r '.tip_height // 0')"
log "after reseed: local_tip=$LOCAL_TIP canonical_tip=$CANON_TIP"

if [[ "$LOCAL_TIP" -lt "$((CANON_TIP - 5))" ]]; then
  log "incremental catch-up via p2p_follower_sync (max 30 loops)"
  ADMIN_TOKEN="$ADMIN" HACKME_ADMIN_TOKEN="$ADMIN" LOOPS=30 \
    bash "$ROOT/scripts/ops/p2p_follower_sync.sh" || true
fi

LOCAL_TIP2="$(curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" 'http://127.0.0.1:8080/api/status?lite=1' | jq -r '.tip_height // 0')"
WALLET="$(curl -fsS --max-time 20 -H "X-Hackme-Admin-Token: $ADMIN" 'http://127.0.0.1:8080/api/wallet' | jq -c '{balance_hmc,balance_on_chain_hmc,wallet_source,balance_alignment}')"
log "final local_tip=$LOCAL_TIP2 wallet=$WALLET"
log "done (local backup: ${BACKUP:-none})"
