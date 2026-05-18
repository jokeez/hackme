#!/usr/bin/env bash
# Verify desktop node + worker miner address matches expected HMC address.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
EXPECTED="${1:-HMC-91fe007e4036c602}"
DATA_DIR="${HACKME_DATA_DIR:-$ROOT/logs/desktop/data}"
got="$(cd "$ROOT" && go run ./tools/show_node_addr "$DATA_DIR")"
if [[ "$got" != "$EXPECTED" ]]; then
  echo "[verify-miner] FAIL data_dir=$DATA_DIR address=$got want=$EXPECTED" >&2
  exit 1
fi
echo "[verify-miner] OK $got (data_dir=$DATA_DIR)"
