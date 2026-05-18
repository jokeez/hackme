#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
AMOUNT_HMC="${AMOUNT_HMC:-1.0}"
TO_ADDR="${TO_ADDR:-HMC-381c0c5e2cfcc714}"
CHAIN_BASE="${CHAIN_BASE:-https://hackme.tech}"
DATA_DIR="${HACKME_DATA_DIR:-$ROOT/logs/desktop/data}"
echo "[fund-treasury] ${AMOUNT_HMC} HMC -> ${TO_ADDR}"
go run ./cmd/sendtransfer -data-dir "$DATA_DIR" -to "$TO_ADDR" -amount-hmc "$AMOUNT_HMC" -base "$CHAIN_BASE" -memo settlement_treasury_topup
