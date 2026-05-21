#!/usr/bin/env bash
# Critical security / resilience pack: WASM sandbox, worker network faults, hybrid signer.
#   bash scripts/tests/critical_security_pack.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd go
OUT="${OUT_DIR:-$ROOT/reports/tests}/${RUN_ID:-$(run_id)}/critical_security_pack"
ensure_reports_dir "$OUT"
LOG="$OUT/run.log"
: >"$LOG"

run_step() {
  local name="$1"
  shift
  echo "=== $name ===" | tee -a "$LOG"
  if "$@" >>"$LOG" 2>&1; then
    pass "$name"
    return 0
  fi
  fail "$name (see $LOG)"
}

run_step "fuzz_wasm_sandbox" go test ./internal/fuzz -count=1 -timeout=120s
run_step "workercoord_network_fault" go test ./internal/workercoord -count=1 -timeout=120s
run_step "worksubmit_payload_tamper" go test ./internal/worksubmit -run TestSignPayload_Tamper -count=1
run_step "coordinator_hybrid_tamper" go test ./cmd/coordinator -run 'HybridSigner.*Tamper|HybridSignerRejectsInvalid' -count=1 -timeout=60s
run_step "sandbox_checkexport" go test ./internal/sandbox -count=1 -timeout=60s

pass "critical_security_pack complete — $OUT"
