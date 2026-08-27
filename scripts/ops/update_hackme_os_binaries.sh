#!/usr/bin/env bash
# O1 — update HackMe OS /opt/hackme binaries in place (no new ISO flash).
# Thin wrapper around update_hackme_miner.sh with INSTALL_DIR=/opt/hackme.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export HACKME_INSTALL_DIR="${HACKME_INSTALL_DIR:-/opt/hackme}"
exec bash "$ROOT/scripts/ops/update_hackme_miner.sh" --install-dir "$HACKME_INSTALL_DIR" "$@"
