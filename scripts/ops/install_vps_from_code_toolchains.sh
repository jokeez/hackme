#!/usr/bin/env bash
# VPS wrapper — delegates to install_from_code_toolchains.sh (root + /opt/hackme).
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec bash "$ROOT_DIR/scripts/ops/install_from_code_toolchains.sh" \
  --system \
  --prefix "${HACKME_PREFIX:-/opt/hackme}" \
  "$@"
