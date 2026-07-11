#!/usr/bin/env bash
# Production pool fuzz audit from VPS only (desktop stays on PoH mining).
#
# Creates campaign on VPS node, runs 3 simulated container workers on VPS, verifies progress.
#
#   NODE_SSH=hackme-vps bash scripts/ops/run_vps_pool_fuzz_audit.sh
#   BUDGET_HMC=1.0 RUNS=32 WORKER_SEC=60 bash scripts/ops/run_vps_pool_fuzz_audit.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NODE_SSH="${NODE_SSH:-hackme-vps}"
BUDGET_HMC="${BUDGET_HMC:-1.0}"
RUNS="${RUNS:-32}"
WORKER_SEC="${WORKER_SEC:-60}"

log() { echo "[vps-fuzz-audit] $*"; }

log "cleanup coordinator queue (gates + stale pending)"
NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/coordinator_fuzz_queue_cleanup.sh"

log "rsync fuzz scripts + wasm to $NODE_SSH"
rsync -az "$ROOT/scripts/ops/run_prod_multi_container_fuzz.sh" \
  "$ROOT/scripts/ops/coordinator_fuzz_queue_cleanup.sh" \
  "${NODE_SSH}:/opt/hackme/scripts/ops/"
rsync -az "$ROOT/tasks/artifacts/security/rust_script_push_bounds_guard.wasm" \
  "${NODE_SSH}:/opt/hackme/tasks/artifacts/security/"

log "multi-container fuzz on VPS (runs=$RUNS budget=$BUDGET_HMC worker_sec=$WORKER_SEC)"
NODE_SSH="$NODE_SSH" BUDGET_HMC="$BUDGET_HMC" RUNS="$RUNS" WORKER_SEC="$WORKER_SEC" \
  bash "$ROOT/scripts/ops/run_prod_multi_container_fuzz.sh"

log "done — desktop workerfuzz should stay OFF; PoH only on main PC"
