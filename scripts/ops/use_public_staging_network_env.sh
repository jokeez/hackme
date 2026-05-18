#!/usr/bin/env bash
# Join the documented public staging command node + coordinator (see README / rc_freeze_gate).
# Usage:  source scripts/ops/use_public_staging_network_env.sh
# Then restart hackme from the repo root (same shell retains exports).

export HACKME_CANONICAL_CHAIN_URL="${HACKME_CANONICAL_CHAIN_URL:-http://132.243.112.100:18080}"
export HACKME_POOL_COORDINATOR_URL="${HACKME_POOL_COORDINATOR_URL:-http://132.243.112.100:18081}"
export HACKME_P2P_PEERS="${HACKME_P2P_PEERS:-http://132.243.112.100:18080}"

echo "[hackme] Using public staging: CANONICAL=$HACKME_CANONICAL_CHAIN_URL COORD=$HACKME_POOL_COORDINATOR_URL P2P=$HACKME_P2P_PEERS" >&2
echo "[hackme] Pool path: dashboard → worker or POST /api/worker/start (local WASM PoH only if HACKME_CHAIN_LEADER_LOCAL_POH=1 on command node)" >&2
