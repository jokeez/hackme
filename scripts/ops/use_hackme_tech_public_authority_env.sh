#!/usr/bin/env bash
# One-line public authority: TLS command node behind https://hackme.tech (nginx).
# Coordinator is inferred as https://hackme.tech/pool/coordinator (see pool.go).
#
# Usage (from repo root):
#   source scripts/ops/use_hackme_tech_public_authority_env.sh
#   go run .   # or your systemd unit
#
# Worker loop (needs coordinator token from the operator, never commit it):
#   source scripts/ops/use_hackme_tech_public_authority_env.sh
#   export COORD_URL="${HACKME_POOL_COORDINATOR_URL}"
#   export COORD_ADMIN_TOKEN='...'
#   export WORKER_ID='my-rig-01'
#   bash scripts/ops/worker_loop.sh

set -euo pipefail

export HACKME_PUBLIC_AUTHORITY_BASE="${HACKME_PUBLIC_AUTHORITY_BASE:-https://hackme.tech}"

echo "[hackme] HACKME_PUBLIC_AUTHORITY_BASE=$HACKME_PUBLIC_AUTHORITY_BASE" >&2
echo "[hackme] After node start: CANONICAL and COORD are set from this base unless already in env." >&2
echo "[hackme] For worker_loop: COORD_URL=https://hackme.tech/pool/coordinator + COORD_ADMIN_TOKEN from VPS operator." >&2
