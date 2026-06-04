#!/usr/bin/env bash
# Build hackme-node locally, rsync to VPS, restart service (same layout as dual_vps_cutover node half).
#
#   NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/deploy_hackme_node.sh
#
# Secrets on the server (/opt/hackme/.env.vps, .env.coord, toolchains) are rsync-excluded and never deleted.
# If .env.vps was removed by an older deploy, recreate it before re-running (see scripts/ops/vps_bootstrap.sh
# or copy from your password manager), then: sudo systemctl restart hackme-node
#
# Optional:
#   DEPLOY_VERSION — override embedded node version (default: RELEASE_VER from web/site/assets/app.js, else "dev")
#   INSTALL_FROM_CODE_TOOLCHAINS=1 — run install_vps_from_code_toolchains.sh on remote after sync
#   SKIP_BUILD=1
#   SKIP_COORDINATOR_BUILD=1 — do not build ./cmd/coordinator (same tree as node on single VPS)
#   SYNC_DIST=1 — include dist/ in rsync (release ZIP/tar for /dist/ URLs); avoids a second SSH/rsync session

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
_OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$_OPS_DIR/_deploy_ssh_retry.sh"

# Optional: HACKME_DEPLOY_SSH_IDENTITY=/path/to/key (chmod 600, never commit)
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

NODE_SSH="${NODE_SSH:-}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
SKIP_BUILD="${SKIP_BUILD:-0}"
SKIP_COORDINATOR_BUILD="${SKIP_COORDINATOR_BUILD:-0}"
INSTALL_FROM_CODE_TOOLCHAINS="${INSTALL_FROM_CODE_TOOLCHAINS:-0}"
SYNC_DIST="${SYNC_DIST:-0}"

if [[ -z "$NODE_SSH" ]]; then
  echo "[deploy-hackme-node] set NODE_SSH=root@host" >&2
  exit 1
fi

for x in ssh rsync curl jq; do
  command -v "$x" >/dev/null || {
    echo "[deploy-hackme-node] missing: $x" >&2
    exit 1
  }
done

DEPLOY_VERSION="${DEPLOY_VERSION:-}"
if [[ -z "$DEPLOY_VERSION" ]] && [[ -f "$ROOT_DIR/web/site/assets/app.js" ]]; then
  DEPLOY_VERSION="$(grep -oE 'const RELEASE_VER = "[^"]+"' "$ROOT_DIR/web/site/assets/app.js" | sed -n 's/.*"\([^"]*\)".*/\1/p')"
fi
if [[ -z "$DEPLOY_VERSION" ]]; then
  DEPLOY_VERSION="dev"
fi
COMMIT_SHA="${DEPLOY_COMMIT:-$(git -C "$ROOT_DIR" rev-parse --short=12 HEAD 2>/dev/null || true)}"
if [[ -z "$COMMIT_SHA" ]] && [[ -f "$ROOT_DIR/dist/release_${DEPLOY_VERSION}/BUILD_INFO.txt" ]]; then
  COMMIT_SHA="$(grep -E '^commit=' "$ROOT_DIR/dist/release_${DEPLOY_VERSION}/BUILD_INFO.txt" | head -1 | cut -d= -f2)"
fi
if [[ -z "$COMMIT_SHA" ]]; then
  COMMIT_SHA="nogit"
fi
BUILD_DATE_UTC="${DEPLOY_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS_NODE="-X main.Version=${DEPLOY_VERSION} -X main.Commit=${COMMIT_SHA} -X main.BuildDate=${BUILD_DATE_UTC}"

if [[ "$SKIP_BUILD" != "1" ]]; then
  echo "[deploy-hackme-node] go build hackme-node (embed version=${DEPLOY_VERSION} commit=${COMMIT_SHA})"
  go build -trimpath -ldflags "-s -w ${LDFLAGS_NODE}" -o hackme-node .
  if [[ "$SKIP_COORDINATOR_BUILD" != "1" ]]; then
    echo "[deploy-hackme-node] go build coordinator"
    go build -trimpath -ldflags "-s -w" -o coordinator ./cmd/coordinator
    echo "[deploy-hackme-node] go build minersign (worker hybrid signer helper)"
    go build -trimpath -ldflags "-s -w" -o minersign ./cmd/minersign
  fi
fi

echo "[deploy-hackme-node] rsync -> $NODE_SSH:$NODE_DEPLOY_DIR (SYNC_DIST=${SYNC_DIST})"
# Never mirror-delete secrets or VPS-local toolchain dirs (rsync --delete would remove them).
RSYNC_EXCLUDES=(
  --exclude '.git/' --exclude 'data/' --exclude 'reports/' --exclude 'node_modules/'
  --exclude 'backups/' --exclude 'logs/' --exclude '*.exe'
  --exclude '.env' --exclude '.env.*'
  --exclude '.cargo/' --exclude '.npm-global/' --exclude '.rustup/'
)
if [[ "$SYNC_DIST" != "1" ]]; then
  RSYNC_EXCLUDES+=(--exclude 'dist/')
fi
deploy_ssh_retry_run _deploy_rsync -az --delete \
  "${RSYNC_EXCLUDES[@]}" \
  "$ROOT_DIR/" "$NODE_SSH:$NODE_DEPLOY_DIR/"

if [[ "$INSTALL_FROM_CODE_TOOLCHAINS" == "1" ]]; then
  echo "[deploy-hackme-node] remote from-code toolchains"
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" "bash -lc '$NODE_DEPLOY_DIR/scripts/ops/install_vps_from_code_toolchains.sh'"
fi

# One SSH session after rsync: fewer TCP connects when port 22 flakes between commands.
echo "[deploy-hackme-node] chmod, restart hackme-node/coordinator, smoke (single SSH)"
deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" bash -s <<REMOTE_EOF
set -euo pipefail
d='$NODE_DEPLOY_DIR'
if [[ ! -f "\$d/.env.vps" ]]; then
  echo "[deploy-hackme-node] FATAL: \$d/.env.vps missing on remote." >&2
  echo "  Previous rsync --delete may have removed it before excludes were added." >&2
  echo "  Recreate .env.vps on the server (HACKME_BIND_ADDR, HACKME_ADMIN_TOKEN, P2P, coordinator URL/token), then re-run this script." >&2
  exit 3
fi
chmod 755 "\$d/hackme-node"
if [[ -f "\$d/coordinator" ]]; then chmod 755 "\$d/coordinator"; fi
systemctl daemon-reload
systemctl restart hackme-node
if systemctl is-active --quiet hackme-coordinator; then systemctl restart hackme-coordinator; fi
sleep 2
systemctl is-active hackme-node
echo "[deploy-hackme-node] smoke: GET /api/status on loopback"
status_json="\$(curl -fsS --max-time 15 http://127.0.0.1:18080/api/status)"
echo "\$status_json" | jq '{tip_height,mining,has_genesis,version,commit}' 2>/dev/null || echo "\$status_json" | head -c 240
echo
if curl -fsS --max-time 5 http://127.0.0.1:18081/health >/dev/null 2>&1; then
  echo "[deploy-hackme-node] coordinator health OK (loopback :18081)"
else
  echo "[deploy-hackme-node] coordinator :18081 not OK from VPS loopback (check service/firewall)"
fi
REMOTE_EOF
