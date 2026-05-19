#!/usr/bin/env bash
# Sync critical ops scripts to your VPS over SSH (you must run this — the AI has no SSH).
#
#   export HACKME_SSH="ubuntu@your.host"
#   export HACKME_REMOTE_ROOT="/opt/hackme"   # optional
#   bash scripts/ops/rsync_vps_hackme_ops.sh
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SSH_TARGET="${HACKME_SSH:-}"
REMOTE="${HACKME_REMOTE_ROOT:-/opt/hackme}"

if [[ -z "$SSH_TARGET" ]]; then
	echo "[rsync-vps] Set HACKME_SSH=user@host (required)." >&2
	exit 2
fi

for f in settle_worker_payouts.sh settlement_healthcheck.sh settlement.env.example desktop_vps_settlement_note.sh \
  sync_settlement_admin_token.sh repair_worker_settlement_state.sh vps_settlement_bootstrap.sh; do
	src="$ROOT/scripts/ops/$f"
	[[ -f "$src" ]] || { echo "[rsync-vps] missing $src" >&2; exit 2; }
	rsync -avz "$src" "$SSH_TARGET:$REMOTE/scripts/ops/"
done

echo "[rsync-vps] uploaded settle scripts → $SSH_TARGET:$REMOTE/scripts/ops/"
echo "[rsync-vps] On server: sudo systemctl restart hackme-worker-settlement.timer"
echo "[rsync-vps] Logs: journalctl -u hackme-worker-settlement.service -n 40 --no-pager"
