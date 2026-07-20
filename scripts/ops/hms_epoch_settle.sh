#!/usr/bin/env bash
# hms_epoch_settle.sh — operator alias for sealed-epoch HMS minting.
# Docs / timers reference this path; implementation lives in settle_worker_hms.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec bash "$ROOT/scripts/ops/settle_worker_hms.sh" "$@"
