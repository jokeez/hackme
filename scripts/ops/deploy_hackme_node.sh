#!/usr/bin/env bash
# Operator-only. Builds a CLEAN commit, ships it to the hub, restarts, and rolls back unless the
# chain keeps producing blocks.
#
#   NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_node.sh              # deploy origin/main
#   DRY_RUN=1 NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_node.sh    # build + show rsync diff only
#   DEPLOY_REF=<sha|tag|branch> …                                           # deploy a specific commit
#
# Safety model (after the 2026-10-27 3.5h chain halt caused by deploying a dirty local tree):
#   1. Build from a throwaway git worktree at DEPLOY_REF (default origin/main) — never from the
#      working directory, so uncommitted edits, crash-* files etc. never reach production.
#      DEPLOY_REF must be on origin/main unless ALLOW_UNPUSHED=1.
#   2. rsync WITHOUT --delete: server-only files (scripts/local/ops, extra bin/*) survive.
#      RSYNC_DELETE=1 opts back into mirror-delete (dangerous: the source is a clean tree).
#   3. Snapshot the live binaries to releases/<stamp>/ before syncing (keeps 5).
#   4. deploy_remote_activate.sh restarts services, then requires +GATE_MIN_BLOCKS blocks within
#      GATE_BLOCK_SEC; otherwise restores the snapshot binaries and exits 6 (ROLLED BACK).
#
# Secrets on the server (.env.vps, .env.coord, toolchains) are rsync-excluded and never touched.
#
# Optional:
#   DEPLOY_VERSION — override embedded node version (default: RELEASE_VER from web/site/assets/app.js)
#   DEPLOY_FROM_LOCAL_TREE=1 — legacy: build and ship the working directory (no clean-commit guarantee)
#   SKIP_BUILD=1 — only with DEPLOY_FROM_LOCAL_TREE=1 (reuse binaries already built there)
#   SKIP_COORDINATOR_BUILD=1 — do not build coordinator / helpers
#   SYNC_DIST=1 — include dist/ in rsync
#   INSTALL_FROM_CODE_TOOLCHAINS=1 — run install_vps_from_code_toolchains.sh on remote after sync
#   SKIP_FLEET_RESTART=1, SKIP_HEALTH_GATE=1, GATE_MIN_BLOCKS, GATE_BLOCK_SEC — see deploy_remote_activate.sh

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
DRY_RUN="${DRY_RUN:-0}"
DEPLOY_FROM_LOCAL_TREE="${DEPLOY_FROM_LOCAL_TREE:-0}"
DEPLOY_REF="${DEPLOY_REF:-origin/main}"
RSYNC_DELETE="${RSYNC_DELETE:-0}"

if [[ -z "$NODE_SSH" ]]; then
  echo "[deploy-hackme-node] set NODE_SSH=root@host" >&2
  exit 1
fi

for x in ssh rsync curl jq git go; do
  command -v "$x" >/dev/null || {
    echo "[deploy-hackme-node] missing: $x" >&2
    exit 1
  }
done

# --- 1. clean source tree -------------------------------------------------------------------
REPO_DIR="$ROOT_DIR"
if [[ "$DEPLOY_FROM_LOCAL_TREE" == "1" ]]; then
  echo "[deploy-hackme-node] WARN: DEPLOY_FROM_LOCAL_TREE=1 — shipping the working directory as-is" >&2
else
  if [[ "$SKIP_BUILD" == "1" ]]; then
    echo "[deploy-hackme-node] SKIP_BUILD=1 needs DEPLOY_FROM_LOCAL_TREE=1 (a fresh worktree has no binaries)" >&2
    exit 1
  fi
  git -C "$REPO_DIR" fetch -q origin
  DEPLOY_SHA="$(git -C "$REPO_DIR" rev-parse --verify "${DEPLOY_REF}^{commit}")"
  if [[ "${ALLOW_UNPUSHED:-0}" != "1" ]] && ! git -C "$REPO_DIR" merge-base --is-ancestor "$DEPLOY_SHA" origin/main; then
    echo "[deploy-hackme-node] FATAL: $DEPLOY_REF ($DEPLOY_SHA) is not on origin/main — push it first or ALLOW_UNPUSHED=1" >&2
    exit 1
  fi
  WORKTREE="$(mktemp -d "${TMPDIR:-/tmp}/hackme-deploy.XXXXXX")"
  # rsync -a "src/" copies the source root's mode onto the deploy dir: mktemp's 0700 made
  # /opt/hackme unreadable for the hackme service user (2026-10-28, 5 min node outage).
  chmod 755 "$WORKTREE"
  cleanup_worktree() { git -C "$REPO_DIR" worktree remove --force "$WORKTREE" >/dev/null 2>&1 || rm -rf "$WORKTREE"; }
  trap cleanup_worktree EXIT
  git -C "$REPO_DIR" worktree add -q --detach "$WORKTREE" "$DEPLOY_SHA"
  ROOT_DIR="$WORKTREE"
  cd "$ROOT_DIR"
  echo "[deploy-hackme-node] clean build of $DEPLOY_REF = $(git -C "$ROOT_DIR" log -1 --format='%h %s')"
fi

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
    echo "[deploy-hackme-node] go build workersettle + workersettlectl (HMC units ledger settler)"
    mkdir -p bin
    go build -trimpath -ldflags "-s -w" -o bin/workersettle ./cmd/workersettle
    go build -trimpath -ldflags "-s -w" -o bin/workersettlectl ./cmd/workersettlectl
    echo "[deploy-hackme-node] go build workerfuzz (dig pool; wasm-validate child entry)"
    go build -trimpath -ldflags "-s -w" -o bin/workerfuzz ./cmd/workerfuzz
  fi
  # No-cgo wasm-validate child for node + coordinator gates (HACKME_WASM_VALIDATE_BIN).
  echo "[deploy-hackme-node] go build hackme-wasm-check (wasm-validate child)"
  mkdir -p bin
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/hackme-wasm-check ./cmd/hackme-wasm-check
  echo "[deploy-hackme-node] go build checkpoint (external chain checkpoints)"
  go build -trimpath -ldflags "-s -w" -o bin/checkpoint ./cmd/checkpoint
fi
if [[ ! -x "$ROOT_DIR/bin/hackme-wasm-check" ]]; then
  echo "[deploy-hackme-node] FATAL: bin/hackme-wasm-check missing — PoH gates would have no validator" >&2
  exit 4
fi

# --- 2. sync ---------------------------------------------------------------------------------
# Never mirror-delete secrets or VPS-local toolchain dirs.
RSYNC_EXCLUDES=(
  --exclude '.git' --exclude '.git/' --exclude 'data/' --exclude 'reports/' --exclude 'node_modules/'
  --exclude 'backups/' --exclude 'releases/' --exclude 'logs/' --exclude '*.exe'
  --exclude 'tasks/artifacts/' --exclude '.cache/'
  --exclude '.env' --exclude '.env.*'
  --exclude '.cargo/' --exclude '.npm-global/' --exclude '.rustup/'
  --exclude '.secrets/' --exclude 'apt/'
  --exclude 'aflpp/'
  --exclude 'web/exchange/' --exclude 'DEPLOYED'
)
if [[ "$SYNC_DIST" != "1" ]]; then
  RSYNC_EXCLUDES+=(--exclude 'dist/')
fi
RSYNC_FLAGS=(-az)
if [[ "$RSYNC_DELETE" == "1" ]]; then
  echo "[deploy-hackme-node] WARN: RSYNC_DELETE=1 — server files absent from the source will be deleted" >&2
  RSYNC_FLAGS+=(--delete)
fi

if [[ "$DRY_RUN" == "1" ]]; then
  echo "[deploy-hackme-node] DRY_RUN=1 — rsync preview, time-only changes hidden (nothing is changed on $NODE_SSH):"
  deploy_ssh_retry_run _deploy_rsync "${RSYNC_FLAGS[@]}" --dry-run --itemize-changes \
    "${RSYNC_EXCLUDES[@]}" "$ROOT_DIR/" "$NODE_SSH:$NODE_DEPLOY_DIR/" | grep -vE '^\.[df]\.\.t\.\.\.\.\.\. ' || true
  echo "[deploy-hackme-node] DRY_RUN done: commit=${COMMIT_SHA} version=${DEPLOY_VERSION}"
  exit 0
fi

# --- 3. snapshot live binaries for rollback ---------------------------------------------------
RELEASE_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
SNAP_DIR="$NODE_DEPLOY_DIR/releases/$RELEASE_STAMP"
echo "[deploy-hackme-node] snapshot live binaries -> $SNAP_DIR"
deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" bash -s -- "$NODE_DEPLOY_DIR" "$SNAP_DIR" <<'SNAP_EOF'
set -euo pipefail
d="$1"; snap="$2"
mkdir -p "$snap/bin"
for b in hackme-node coordinator minersign bin/workersettle bin/workersettlectl bin/workerfuzz bin/hackme-wasm-check bin/checkpoint; do
  [[ -f "$d/$b" ]] && cp -a "$d/$b" "$snap/$b"
done
[[ -f "$d/DEPLOYED" ]] && cp -a "$d/DEPLOYED" "$snap/DEPLOYED"
# keep the newest 5 snapshots
ls -1d "$d"/releases/*/ 2>/dev/null | sort | head -n -5 | xargs -r rm -rf
echo "[deploy-hackme-node] snapshot: $(ls "$snap" "$snap/bin" | tr '\n' ' ')"
SNAP_EOF

echo "[deploy-hackme-node] rsync -> $NODE_SSH:$NODE_DEPLOY_DIR (delete=${RSYNC_DELETE} dist=${SYNC_DIST})"
deploy_ssh_retry_run _deploy_rsync "${RSYNC_FLAGS[@]}" \
  "${RSYNC_EXCLUDES[@]}" \
  "$ROOT_DIR/" "$NODE_SSH:$NODE_DEPLOY_DIR/"

if [[ "$INSTALL_FROM_CODE_TOOLCHAINS" == "1" ]]; then
  echo "[deploy-hackme-node] remote from-code toolchains"
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" "bash -lc '$NODE_DEPLOY_DIR/scripts/ops/install_vps_from_code_toolchains.sh'"
fi

# --- 4. activate + chain-progress gate + automatic rollback --------------------------------
echo "[deploy-hackme-node] activate (restart, gate on new blocks, rollback on failure)"
_deploy_ssh "$NODE_SSH" env \
  SKIP_FLEET_RESTART="${SKIP_FLEET_RESTART:-0}" SKIP_HEALTH_GATE="${SKIP_HEALTH_GATE:-0}" \
  GATE_MIN_BLOCKS="${GATE_MIN_BLOCKS:-2}" GATE_BLOCK_SEC="${GATE_BLOCK_SEC:-300}" \
  bash "$NODE_DEPLOY_DIR/scripts/ops/deploy_remote_activate.sh" \
  "$NODE_DEPLOY_DIR" "$SNAP_DIR" "$COMMIT_SHA" "$DEPLOY_VERSION"
echo "[deploy-hackme-node] done: commit=${COMMIT_SHA} version=${DEPLOY_VERSION}"
