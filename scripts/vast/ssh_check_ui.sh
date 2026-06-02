#!/usr/bin/env bash
# Local coordinator snapshot for worker_id (compare with hackme.tech dashboard).
# Usage: bash scripts/vast/ssh_check_ui.sh <worker_id>
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WID="${1:-}"
[[ -n "$WID" ]] || { echo "usage: $0 <worker_id>"; exit 1; }

if [[ -f "$ROOT/.secrets/vast/instances.json" ]]; then
  export COORD_URL="$(jq -r '.coord_url // "https://hackme.tech/pool/coordinator"' "$ROOT/.secrets/vast/instances.json")"
fi
if [[ -f "$ROOT/dist/vast-gpu-matrix-"*/env.vast ]]; then
  pack_env="$(ls -1t "$ROOT"/dist/vast-gpu-matrix-*/env.vast 2>/dev/null | head -1)"
  # shellcheck disable=SC1090
  source "$pack_env" 2>/dev/null || true
fi
export WORKER_ID="$WID"
export REPORT="$ROOT/reports/vast-remote/ui-check-$WID"
mkdir -p "$REPORT"
bash "$SCRIPT_DIR/03_ui_snapshot.sh"
