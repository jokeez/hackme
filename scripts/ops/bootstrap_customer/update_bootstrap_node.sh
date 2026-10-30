#!/usr/bin/env bash
# Build linux node from current tree and restart hackme-bootstrap-node.
# Keeps customer settle pull able to apply new kinds (e.g. run_floor).
#
#   bash scripts/ops/bootstrap_customer/update_bootstrap_node.sh
#   BOOTSTRAP_SSH=hackme-bootstrap bash …
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
HOST="${BOOTSTRAP_SSH:-hackme-bootstrap}"
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
REMOTE_BIN="${INSTALL}/hackme"

cd "$ROOT"
echo "[bootstrap-node] go build -> /tmp/hackme-bootstrap-node"
go build -trimpath -o /tmp/hackme-bootstrap-node .
scp -o BatchMode=yes /tmp/hackme-bootstrap-node "${HOST}:${REMOTE_BIN}.new"
ssh -o BatchMode=yes "$HOST" "bash -s" <<EOF
set -euo pipefail
stamp=\$(date -u +%Y%m%dT%H%M%SZ)
cp -a '$REMOTE_BIN' '${REMOTE_BIN}.bak-'"\$stamp" || true
mv '${REMOTE_BIN}.new' '$REMOTE_BIN'
chmod +x '$REMOTE_BIN'
systemctl restart hackme-bootstrap-node
sleep 2
systemctl is-active hackme-bootstrap-node
curl -fsS --max-time 8 http://127.0.0.1:8080/api/status >/dev/null
echo "[bootstrap-node] OK"
EOF
