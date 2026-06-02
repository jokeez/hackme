#!/usr/bin/env bash
# Summarize session for GPU_MATRIX_SHEET + tarball hint.
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT="${REPORT:-$PACK_ROOT/reports/vast-session}"
LOG="$REPORT/worker.log"
SHEET="$PACK_ROOT/GPU_MATRIX_SHEET.csv"

gpu_name="$(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null | head -1 | tr -d '\r' || echo unknown)"
driver="$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -1 | tr -d '\r' || echo ?)"
ccap="$(nvidia-smi --query-gpu=compute_cap --format=csv,noheader 2>/dev/null | head -1 | tr -d '\r' || echo ?)"
vram="$(nvidia-smi --query-gpu=memory.total --format=csv,noheader 2>/dev/null | head -1 | tr -d '\r' || echo ?)"

ghs_lines="$(grep -Ei 'GH/s|gh/s|hashrate|calibrat' "$LOG" 2>/dev/null | tail -20 || true)"
ghs_peak="$(echo "$ghs_lines" | grep -oE '[0-9]+(\.[0-9]+)?[[:space:]]*GH/s' | head -1 | grep -oE '[0-9.]+' || echo "")"
pass=PASS
fail=""
if ! grep -qiE 'submit.*ok|found|accepted|hit' "$LOG" 2>/dev/null; then
  if [[ ! -s "$LOG" ]]; then
    pass=FAIL
    fail="empty log"
  else
    pass=WARN
    fail="no obvious submit/hit in log — review manually"
  fi
fi

wid="${WORKER_ID:-vast-unknown}"
if [[ -f "$PACK_ROOT/env.vast" ]]; then
  # shellcheck disable=SC1091
  source "$PACK_ROOT/env.vast"
fi

echo ""
echo "=== Vast session summary ==="
echo "worker_id: $wid"
echo "gpu: $gpu_name | driver $driver | cc $ccap | vram $vram"
echo "peak_ghs_guess: ${ghs_peak:-—}"
echo "verdict: $pass ${fail:+(}$fail)}"
echo "log: $LOG"
echo ""
echo "Append row to GPU_MATRIX_SHEET.csv on your laptop."

ARCHIVE="$REPORT/vast-report-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
tar -czf "$ARCHIVE" -C "$PACK_ROOT" reports/vast-session env.vast 2>/dev/null || \
  tar -czf "$ARCHIVE" -C "$REPORT" . 2>/dev/null || true
echo "[collect] optional bundle: $ARCHIVE (scp to desktop)"
