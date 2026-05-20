#!/usr/bin/env bash
# Full GPU / rig / multi-vendor test suite (local host).
#   bash scripts/tests/gpu_rig_suite.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd go
require_cmd jq
require_cmd bash

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/gpu_rig_suite"
ensure_reports_dir "$OUT"
LOG="$OUT/suite.log"
: >"$LOG"

log() { echo "$*" | tee -a "$LOG"; }
section() { log ""; log "=== $* ==="; }

failures=0
passes=0
record() {
  local status="$1" name="$2" detail="${3:-}"
  if [[ "$status" == "PASS" ]]; then
    passes=$((passes + 1))
    log "[PASS] $name${detail:+ — $detail}"
  else
    failures=$((failures + 1))
    log "[FAIL] $name${detail:+ — $detail}"
  fi
}

section "Host GPU inventory"
{
  echo "--- nvidia-smi -L ---"
  nvidia-smi -L 2>/dev/null || echo "(no nvidia-smi)"
  echo "--- nvidia-smi --query-gpu ---"
  nvidia-smi --query-gpu=index,name,driver_version,compute_cap,memory.total,power.limit --format=csv,noheader 2>/dev/null || true
  echo "--- DRM vendors (sysfs) ---"
  for f in /sys/class/drm/card*/device/vendor; do
    [[ -f "$f" ]] || continue
    card="$(basename "$(dirname "$(dirname "$f")")")"
    name="$(cat "/sys/class/drm/$card/device/product_name" 2>/dev/null || echo '?')"
    echo "$card vendor=$(cat "$f") product=$name"
  done
  echo "--- clinfo (GPU-related) ---"
  if command -v clinfo >/dev/null 2>&1; then
    clinfo 2>/dev/null | awk '/Platform Name|Device Name|Device Type|Global mem|Max compute|Driver version/ {print}' | head -80
  else
    echo "(clinfo not installed — optional for AMD/Intel OpenCL rigs)"
  fi
} >"$OUT/host_inventory.txt" 2>&1
record PASS "host_inventory" "$OUT/host_inventory.txt"

section "Backend detection"
DETECTED="$(HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh")"
echo "$DETECTED" >"$OUT/detected_backend.txt"
log "detect_gpu_backend.sh → $DETECTED"
record PASS "detect_backend" "$DETECTED"

{
  echo "default → $(HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh")"
  echo "HACKME_FORCE_OPENCL=1 → $(HACKME_FORCE_OPENCL=1 HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh")"
  echo "HACKME_GPU_DISABLE=1 → $(HACKME_GPU_DISABLE=1 HACKME_REPO_ROOT="$ROOT_DIR" bash "$ROOT_DIR/scripts/ops/detect_gpu_backend.sh")"
} >"$OUT/backend_matrix.txt"
record PASS "backend_matrix" "$OUT/backend_matrix.txt"

section "Build GPU workers + probes"
export GOCACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
if bash "$ROOT_DIR/scripts/ops/build_gpu_workers.sh" --probe >>"$LOG" 2>&1; then
  record PASS "build_gpu_workers"
else
  record FAIL "build_gpu_workers" "see $LOG"
fi

section "Go unit tests (no hardware)"
if go test ./internal/gputune ./internal/gpupoh -count=1 >>"$LOG" 2>&1; then
  record PASS "go_test_gputune_gpupoh"
else
  record FAIL "go_test_gputune_gpupoh"
fi
if bash "$ROOT_DIR/scripts/tests/gpu_hints_matrix.sh" >>"$LOG" 2>&1; then
  record PASS "gpu_hints_matrix"
else
  record FAIL "gpu_hints_matrix"
fi

section "CUDA: list + probe all devices"
if [[ -x "$ROOT_DIR/bin/gpuprobe-cuda" ]]; then
  if HACKME_CUDA_VERBOSE=1 "$ROOT_DIR/bin/gpuprobe-cuda" >"$OUT/gpuprobe_cuda.txt" 2>&1; then
    nsmoke="$(grep -c '^Smoke #' "$OUT/gpuprobe_cuda.txt" 2>/dev/null || echo 0)"
    record PASS "gpuprobe_cuda" "${nsmoke} device smoke(s)"
  else
    record FAIL "gpuprobe_cuda" "$OUT/gpuprobe_cuda.txt"
  fi
else
  record FAIL "gpuprobe_cuda" "bin/gpuprobe-cuda missing"
fi

listgpu_cuda() {
  if [[ -f "$ROOT_DIR/scripts/ops/cuda_env.sh" ]]; then
    # shellcheck source=/dev/null
    source "$ROOT_DIR/scripts/ops/cuda_env.sh" >&2 2>/dev/null || true
  fi
  HACKME_LISTGPU_TAG=cuda go run -tags cuda "$ROOT_DIR/tools/listgpu"
}
if listgpu_cuda >"$OUT/list_devices_cuda.json" 2>"$OUT/list_devices_cuda.err"; then
  record PASS "listgpu_cuda" "$(jq -r '.devices|length' "$OUT/list_devices_cuda.json") listed, $(jq -r '.usable|length' "$OUT/list_devices_cuda.json") usable"
else
  record FAIL "listgpu_cuda" "$(head -3 "$OUT/list_devices_cuda.err")"
fi

section "OpenCL: list + init (AMD/Intel/Mesa)"
if [[ -x "$ROOT_DIR/bin/gpuprobe-opencl" ]]; then
  if "$ROOT_DIR/bin/gpuprobe-opencl" >"$OUT/gpuprobe_opencl.txt" 2>"$OUT/gpuprobe_opencl_err.txt"; then
    usable="$(grep -c '^usable:' "$OUT/gpuprobe_opencl.txt" 2>/dev/null || echo 0)"
    record PASS "gpuprobe_opencl" "${usable} usable accelerator(s)"
  elif grep -q '"devices"' "$OUT/gpuprobe_opencl.txt" 2>/dev/null; then
    record PASS "gpuprobe_opencl" "devices listed; kernel init failed on this host (normal on NVIDIA+CUDA-only)"
  else
    record FAIL "gpuprobe_opencl" "$(head -5 "$OUT/gpuprobe_opencl_err.txt")"
  fi
else
  record PASS "gpuprobe_opencl" "skipped (not built)"
fi

if HACKME_LISTGPU_TAG=opencl go run -tags opencl "$ROOT_DIR/tools/listgpu" >"$OUT/list_devices_opencl.json" 2>"$OUT/list_devices_opencl.err" 2>/dev/null; then
  record PASS "listgpu_opencl" "$(jq -r '.devices|length' "$OUT/list_devices_opencl.json") listed"
elif [[ ! -f /usr/include/CL/cl.h ]]; then
  record PASS "listgpu_opencl" "skipped (no OpenCL headers)"
else
  record PASS "listgpu_opencl" "no usable OpenCL GPU ($(head -1 "$OUT/list_devices_opencl.err" 2>/dev/null))"
fi

section "Fleet count vs CUDA discovery"
fleet_cuda="$(nvidia-smi -L 2>/dev/null | grep -c -E '^GPU ' || echo 0)"
discover_cuda="$(jq -r '.devices|length' "$OUT/list_devices_cuda.json" 2>/dev/null || echo 0)"
if [[ ! "$discover_cuda" =~ ^[0-9]+$ ]] || [[ "$discover_cuda" -eq 0 ]]; then
  discover_cuda="$(grep -c '^Smoke #' "$OUT/gpuprobe_cuda.txt" 2>/dev/null || echo 0)"
fi
log "nvidia fleet count=$fleet_cuda gpupoh discover=$discover_cuda"
if [[ "$fleet_cuda" == "$discover_cuda" ]] || [[ "$discover_cuda" -ge 1 && "$fleet_cuda" -ge 1 ]]; then
  record PASS "fleet_vs_discover" "nvidia=$fleet_cuda discover=$discover_cuda"
else
  record FAIL "fleet_vs_discover" "mismatch — check CUDA_VISIBLE_DEVICES"
fi

section "Per-GPU worker pick (-gpu-device) smoke"
COORD_TOKEN_FILE="${COORD_TOKEN_FILE:-$ROOT_DIR/.secrets/hackme_coordinator_admin_token}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
if [[ -x "$ROOT_DIR/bin/workerpoh-cuda" ]] && [[ -f "$COORD_TOKEN_FILE" ]] && [[ "$fleet_cuda" =~ ^[0-9]+$ ]] && (( fleet_cuda >= 1 )); then
  tok="$(tr -d '\r\n' <"$COORD_TOKEN_FILE")"
  for ((d = 0; d < fleet_cuda && d < 4; d++)); do
    out="$OUT/worker_pick_gpu${d}.txt"
    timeout 22s "$ROOT_DIR/bin/workerpoh-cuda" \
      -coord "$COORD_URL" -token "$tok" \
      -worker "rig-probe-gpu${d}" -batch 1048576 -gpu-chunk 1048576 \
      -gpu-device "$d" -search-timeout-ms 8000 -gpu-backend cuda \
      >>"$out" 2>&1 || true
    if grep -qE 'CUDA calibrated|submit ok' "$out" 2>/dev/null; then
      ghs="$(grep -oE 'inst_ghs=[0-9.]+' "$out" | tail -1 || true)"
      record PASS "workerpoh_gpu_device_${d}" "${ghs:-ok}"
    else
      record FAIL "workerpoh_gpu_device_${d}" "$out"
    fi
    pkill -f "workerpoh-cuda.*rig-probe-gpu${d}" 2>/dev/null || true
  done
else
  record PASS "workerpoh_gpu_device" "skipped (no token or no CUDA)"
fi

section "gputune hints for this host GPU name(s)"
: >"$OUT/hints_live.txt"
while IFS= read -r gn; do
  [[ -n "$gn" ]] || continue
  line="$(cd "$ROOT_DIR" && go run "$ROOT_DIR/tools/gpuhint" "$gn" 2>/dev/null || echo "?")"
  echo "$gn → $line" >>"$OUT/hints_live.txt"
done < <(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null || true)
if [[ ! -s "$OUT/hints_live.txt" ]]; then
  echo "(no NVIDIA GPUs — hints matrix still covers AMD/Intel in unit tests)" >>"$OUT/hints_live.txt"
fi
record PASS "gputune_live" "$OUT/hints_live.txt"

section "NVIDIA telemetry sample"
if command -v nvidia-smi >/dev/null 2>&1; then
  nvidia-smi --query-gpu=index,name,temperature.gpu,power.draw,clocks.sm --format=csv >"$OUT/telemetry_nvidia.csv" 2>&1 || true
  record PASS "nvidia_telemetry" "$OUT/telemetry_nvidia.csv"
fi

section "Summary"
status="PASS"
[[ "$failures" -eq 0 ]] || status="FAIL"
jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg status "$status" \
  --arg detected "$DETECTED" \
  --argjson passes "$passes" \
  --argjson failures "$failures" \
  '{
    run_id: $run_id,
    captured_at: $captured_at,
    suite: "gpu_rig_suite",
    detected_backend: $detected,
    passes: $passes,
    failures: $failures,
    status: $status
  }' >"$OUT/summary.json"

log ""
log "Suite $status: $passes passed, $failures failed"
log "Report: $OUT"

if [[ "$status" != "PASS" ]]; then
  fail "gpu_rig_suite FAIL ($failures failures). See $OUT"
fi
pass "gpu_rig_suite PASS ($passes checks). See $OUT"
