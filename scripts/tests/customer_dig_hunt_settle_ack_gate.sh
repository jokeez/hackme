#!/usr/bin/env bash
# Product path gate (local units + existing harnesses):
#   Dig/Hunt honesty units → harness publish → hunt pool smoke → settle ACK contract.
# Full docker Dig→Hunt→payout is optional via RUN_DOCKER_E2E=1 (slow).
#
#   bash scripts/tests/customer_dig_hunt_settle_ack_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[customer-e2e] engine policy + floor_only + unpaid + settle ACK units"
go test -count=1 ./internal/hunt/ -run 'ControlledLfAflMix|Monochrome|RunnerAvailable|InventoryForces' -timeout=60s
go test -count=1 ./internal/poolfuzz/ -run 'DecideHuntEnginePay|FloorOnly|ByteFallback' -timeout=60s
go test -count=1 . -run 'SettlementUnpaid|RequireAdminAuthStrict' -timeout=60s
go test -count=1 ./cmd/coordinator/ -run 'Settle|confirm_applied|ConfirmApplied|OrdersPublish' -timeout=90s

echo "[customer-e2e] harness publish gate"
bash "$ROOT/scripts/tests/hunt_harness_publish_gate.sh"

echo "[customer-e2e] hunt pool smoke (Linux LF path)"
bash "$ROOT/scripts/tests/hunt_pool_smoke_gate.sh"

echo "[customer-e2e] windows OOTB honesty"
bash "$ROOT/scripts/ops/windows_miner_ootb_checklist.sh"

if [[ "${RUN_DOCKER_E2E:-0}" == "1" ]]; then
  echo "[customer-e2e] docker Dig settle smoke (optional)"
  bash "$ROOT/scripts/ops/docker_e2e_smoke.sh"
fi

pass "customer_dig_hunt_settle_ack_gate PASS"
