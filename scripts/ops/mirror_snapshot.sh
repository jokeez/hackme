#!/usr/bin/env bash
# Push hub chain + ops state to a warm mirror VPS (rsync over SSH).
#
#   MIRROR_SSH=hackme-mirror bash scripts/ops/mirror_snapshot.sh
set -euo pipefail

NODE_SSH="${NODE_SSH:-hackme-vps}"
MIRROR_SSH="${MIRROR_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
MIRROR_DEPLOY="${MIRROR_DEPLOY_DIR:-/opt/hackme}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
LOG_TAG="[mirror-snapshot ${STAMP}]"
if [[ -z "$MIRROR_SSH" ]]; then
  echo "$LOG_TAG ERROR: set MIRROR_SSH (e.g. hackme-mirror)" >&2
  exit 1
fi

echo "$LOG_TAG source=${NODE_SSH}:${DEPLOY} dest=${MIRROR_SSH}:${MIRROR_DEPLOY}"

ssh -o BatchMode=yes "$MIRROR_SSH" "sudo systemctl stop hackme-node hackme-coordinator 2>/dev/null || true"
ssh -o BatchMode=yes "$MIRROR_SSH" "mkdir -p ${MIRROR_DEPLOY}/data/mirror-meta ${MIRROR_DEPLOY}/web/site ${MIRROR_DEPLOY}/scripts/ops"

# Quiesce WAL on hub so the streamed DB is self-contained.
ssh -o BatchMode=yes "$NODE_SSH" \
  "sudo -u hackme sqlite3 ${DEPLOY}/data/hackme.db 'PRAGMA wal_checkpoint(TRUNCATE);' >/dev/null 2>&1 || true"

# Drop stale sidecar files on mirror before replace.
ssh -o BatchMode=yes "$MIRROR_SSH" \
  "rm -f ${MIRROR_DEPLOY}/data/hackme.db-wal ${MIRROR_DEPLOY}/data/hackme.db-shm"

# Optional: keep mirror binary current with hub (warm standby).
if [[ "${MIRROR_SYNC_BINARIES:-1}" == "1" ]]; then
  echo "$LOG_TAG sync hackme-node binary"
  ssh -o BatchMode=yes "$NODE_SSH" "cat ${DEPLOY}/hackme-node" \
    | ssh -o BatchMode=yes "$MIRROR_SSH" \
      "cat > ${MIRROR_DEPLOY}/hackme-node.new && chmod +x ${MIRROR_DEPLOY}/hackme-node.new && mv -f ${MIRROR_DEPLOY}/hackme-node.new ${MIRROR_DEPLOY}/hackme-node && sudo chown hackme:hackme ${MIRROR_DEPLOY}/hackme-node"
fi

# Copy critical state files through local pipe (source remote -> destination remote).
for f in hackme.db worker_settlement_state.json pool_subsidy_budget_state.json; do
  ssh -o BatchMode=yes "$NODE_SSH" "cat ${DEPLOY}/data/${f}" \
    | ssh -o BatchMode=yes "$MIRROR_SSH" "cat > ${MIRROR_DEPLOY}/data/${f}"
done

# Copy static site and ops scripts via tar stream.
# Hub site can change during tar (news deploy); treat exit 1 "file changed" as success.
ssh -o BatchMode=yes "$NODE_SSH" "tar --warning=no-file-changed -C ${DEPLOY}/web -cf - site" \
  | ssh -o BatchMode=yes "$MIRROR_SSH" "tar -C ${MIRROR_DEPLOY}/web -xf -"

ssh -o BatchMode=yes "$NODE_SSH" "tar --warning=no-file-changed -C ${DEPLOY}/scripts -cf - ops" \
  | ssh -o BatchMode=yes "$MIRROR_SSH" "tar -C ${MIRROR_DEPLOY}/scripts -xf -"

ssh -o BatchMode=yes "$MIRROR_SSH" "printf '%s\n' '${STAMP}' >${MIRROR_DEPLOY}/data/mirror-meta/last_snapshot_utc.txt; sudo chown -R hackme:hackme ${MIRROR_DEPLOY}/data 2>/dev/null || true; sudo systemctl start hackme-node 2>/dev/null || true"

src_size="$(ssh -o BatchMode=yes "$NODE_SSH" "stat -c '%s' ${DEPLOY}/data/hackme.db")"
dst_size="$(ssh -o BatchMode=yes "$MIRROR_SSH" "stat -c '%s' ${MIRROR_DEPLOY}/data/hackme.db")"
if [[ -z "$src_size" || -z "$dst_size" || "$dst_size" -lt 1048576 ]]; then
  echo "$LOG_TAG ERROR: suspicious mirror DB size src=${src_size:-?} dst=${dst_size:-?}" >&2
  exit 1
fi
if [[ "$dst_size" -lt $((src_size / 2)) ]]; then
  echo "$LOG_TAG WARN: mirror DB significantly smaller than source src=$src_size dst=$dst_size" >&2
fi

echo "$LOG_TAG OK stamp=${STAMP}"
