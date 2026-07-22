#!/usr/bin/env bash
# Shared SSH helpers — sources gitignored .secrets/vast/instances.json only.
set -euo pipefail

VAST_SECRETS="${VAST_SECRETS:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/.secrets/vast}"
INSTANCES_JSON="${INSTANCES_JSON:-$VAST_SECRETS/instances.json}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REMOTE_REPORTS="reports/vast-remote"

vast_log() { echo "[vast-ssh] $*" | tee -a "${LOG_FILE:-/dev/stderr}"; }

require_instances() {
  if [[ ! -f "$INSTANCES_JSON" ]]; then
    echo "missing $INSTANCES_JSON — see docs/VAST_SSH_OPERATOR.md" >&2
    exit 2
  fi
}

instance_field() {
  local name="$1" field="$2"
  jq -r --arg n "$name" '
    .instances[] | select(.name==$n) | .'"$field"'
  ' "$INSTANCES_JSON" | head -1
}

resolve_pack() {
  local explicit
  explicit="$(jq -r '.pack_tarball // empty' "$INSTANCES_JSON" 2>/dev/null || true)"
  if [[ -n "$explicit" && -f "$ROOT/$explicit" ]]; then
    echo "$ROOT/$explicit"
    return 0
  fi
  local latest
  latest="$(ls -1t "$ROOT"/dist/vast-gpu-matrix-*.tar.gz 2>/dev/null | head -1 || true)"
  if [[ -n "$latest" ]]; then
    echo "$latest"
    return 0
  fi
  echo "[vast-ssh] no pack tarball — run: bash scripts/ops/pack_vast_gpu_matrix.sh --include-token" >&2
  return 1
}

ssh_opts() {
  local key="$1"
  local kh="${VAST_SECRETS}/known_hosts"
  if [[ ! -f "$kh" ]]; then
    echo "[vast-ssh] missing $kh — pin host keys before connect (StrictHostKeyChecking=yes)" >&2
    exit 2
  fi
  printf '%s\n' \
    -o "StrictHostKeyChecking=yes" \
    -o "UserKnownHostsFile=$kh" \
    -o "ConnectTimeout=30" \
    -i "$key"
}

remote_exec() {
  local host="$1" port="$2" user="$3" key="$4"
  shift 4
  ssh "$(ssh_opts "$key")" -p "$port" "${user}@${host}" "$@"
}

remote_upload_pack() {
  local host="$1" port="$2" user="$3" key="$4" pack="$5"
  local base
  base="$(basename "$pack")"
  vast_log "upload $base -> ~/"
  scp "$(ssh_opts "$key")" -P "$port" "$pack" "${user}@${host}:~/$base"
  remote_exec "$host" "$port" "$user" "$key" \
    "rm -rf ~/vast-gpu-matrix-active && mkdir -p ~/vast-gpu-matrix-active && tar xzf ~/$base -C ~/vast-gpu-matrix-active --strip-components=1"
}
