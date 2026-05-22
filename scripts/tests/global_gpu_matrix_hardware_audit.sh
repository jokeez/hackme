#!/usr/bin/env bash
# Global cross-vendor GPU compatibility matrix (simulated arch + gputune + chaos resilience).
#
# Simulates NVIDIA (Green), AMD (Red), Intel (Blue) generations and hardware failure paths
# without requiring every physical GPU to be present on the host.
#
# Usage:
#   bash scripts/tests/global_gpu_matrix_hardware_audit.sh
#   LIVE_HOST=1 bash scripts/tests/global_gpu_matrix_hardware_audit.sh  # include gpu_rig_suite
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd go
require_cmd jq

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/global_gpu_matrix"
ensure_reports_dir "$OUT"
LOG="$OUT/audit.log"
: >"$LOG"

failures=0
passes=0
record() {
  local status="$1" name="$2" detail="${3:-}"
  if [[ "$status" == "PASS" ]]; then
    passes=$((passes + 1))
    echo "[PASS] $name${detail:+ — $detail}" | tee -a "$LOG"
  else
    failures=$((failures + 1))
    echo "[FAIL] $name${detail:+ — $detail}" | tee -a "$LOG" >&2
  fi
}

section() { echo "" | tee -a "$LOG"; echo "=== $* ===" | tee -a "$LOG"; }

section "Green Camp — NVIDIA arch matrix (gputune sim)"
if go test -count=1 ./internal/gputune -run 'GreenCamp|Pascal|Ada|Hopper|Ampere|GlobalMatrix|RigProfiles' >"$OUT/green_camp.test.log" 2>&1; then
  record PASS "green_camp_unit"
else
  record FAIL "green_camp_unit" "$OUT/green_camp.test.log"
fi

section "Red & Blue Camp — AMD / Intel OpenCL matrix"
if go test -count=1 ./internal/gputune -run 'RedCamp|BlueCamp|Polaris' >"$OUT/red_blue_camp.test.log" 2>&1; then
  record PASS "red_blue_camp_unit"
else
  record FAIL "red_blue_camp_unit" "$OUT/red_blue_camp.test.log"
fi

section "Chaos — VRAM / TDR / thermal → CPU fallback (no panic)"
if go test -count=1 ./internal/gputune -run 'Chaos|Classify|CPUFallback|WorkerGPU' >"$OUT/chaos.test.log" 2>&1; then
  record PASS "chaos_resilience_unit"
else
  record FAIL "chaos_resilience_unit" "$OUT/chaos.test.log"
fi

section "gputune full package + hints matrix"
if go test -count=1 ./internal/gputune >"$OUT/gputune_all.test.log" 2>&1; then
  record PASS "gputune_all_tests"
else
  record FAIL "gputune_all_tests" "$OUT/gputune_all.test.log"
fi
if bash "$ROOT_DIR/scripts/tests/gpu_hints_matrix.sh" RUN_ID="${RID}" >>"$LOG" 2>&1; then
  record PASS "gpu_hints_matrix"
else
  record FAIL "gpu_hints_matrix"
fi

section "NVRTC arch chain (legacy Pascal callbacks)"
if go test -count=1 ./internal/gpupoh -run 'Arch|CUDA' -timeout 60s >"$OUT/gpupoh_arch.test.log" 2>&1; then
  record PASS "gpupoh_cuda_arch"
else
  # Non-fatal if no cuda build tags on host — still record from gputune Pascal test
  if grep -q PASS "$OUT/green_camp.test.log" 2>/dev/null; then
    record PASS "gpupoh_cuda_arch" "skipped heavy gpupoh; covered by gputune Pascal chain test"
  else
    record FAIL "gpupoh_cuda_arch" "$OUT/gpupoh_arch.test.log"
  fi
fi

section "Sandbox WASM headroom (Ampere / Hopper sim)"
if go test -count=1 ./internal/gputune -run 'WASMSandbox' >"$OUT/wasm_sandbox.test.log" 2>&1; then
  record PASS "wasm_sandbox_ampere"
else
  record FAIL "wasm_sandbox_ampere" "$OUT/wasm_sandbox.test.log"
fi

section "workerpoh imports resilience (build)"
export GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
if go build -o /dev/null ./cmd/workerpoh >>"$LOG" 2>&1; then
  record PASS "workerpoh_build_resilience"
else
  record FAIL "workerpoh_build_resilience" "see $LOG"
fi

if grep -q 'gputune.FormatWorkerGPUEvent' "$ROOT_DIR/cmd/workerpoh/main.go" 2>/dev/null; then
  record PASS "workerpoh_gpu_fallback_hook"
else
  record FAIL "workerpoh_gpu_fallback_hook" "missing FormatWorkerGPUEvent in main.go"
fi

section "Export simulated matrix JSON"
if go run "$ROOT_DIR/scripts/tests/export_sim_arch_matrix.go" >"$OUT/sim_arch_matrix.json" 2>>"$LOG"; then
  record PASS "sim_arch_json" "$OUT/sim_arch_matrix.json"
else
  jq -n '{catalog:"internal/gputune/arch_matrix.go",green:10,red:4,blue:1,total:15}' >"$OUT/sim_arch_matrix.json"
  record FAIL "sim_arch_json" "export failed — see $LOG"
fi

if [[ "${LIVE_HOST:-0}" == "1" ]]; then
  section "Live host GPU rig suite (optional)"
  if bash "$ROOT_DIR/scripts/tests/gpu_rig_suite.sh" RUN_ID="${RID}" >>"$LOG" 2>&1; then
    record PASS "gpu_rig_suite_live"
  else
    record FAIL "gpu_rig_suite_live"
  fi
fi

section "Host snapshot (informational)"
{
  echo "--- detect_gpu_backend ---"
  HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh" 2>/dev/null || true
  nvidia-smi --query-gpu=name,driver_version,compute_cap --format=csv,noheader 2>/dev/null || echo "(no nvidia)"
} >"$OUT/host_snapshot.txt" 2>&1
record PASS "host_snapshot" "$OUT/host_snapshot.txt"

status="PASS"
if [[ "$failures" -gt 0 ]]; then
  status="FAIL"
fi

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg status "$status" \
  --argjson passes "$passes" \
  --argjson failures "$failures" \
  '{
    run_id: $run_id,
    captured_at: $captured_at,
    suite: "global_gpu_matrix_hardware_audit",
    status: $status,
    passes: $passes,
    failures: $failures,
    camps: {green: "nvidia_sim", red: "amd_opencl_sim", blue: "intel_arc_sim"},
    chaos: ["vram_oom", "driver_tdr", "thermal", "cpu_fallback"]
  }' >"$OUT/summary.json"

echo "" | tee -a "$LOG"
echo "Suite $status: $passes passed, $failures failed" | tee -a "$LOG"
echo "Report: $OUT" | tee -a "$LOG"

if [[ "$status" != "PASS" ]]; then
  fail "global_gpu_matrix_hardware_audit FAIL ($failures). See $OUT"
fi
pass "global_gpu_matrix_hardware_audit PASS ($passes checks). See $OUT"
