#!/usr/bin/env bash
# GPU inventory on Vast instance — run first.
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT="${REPORT:-$PACK_ROOT/reports/vast-session}"
mkdir -p "$REPORT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$REPORT/inventory-$STAMP.txt"

{
  echo "=== vast inventory $STAMP ==="
  echo "hostname: $(hostname -f 2>/dev/null || hostname)"
  echo "uname: $(uname -a)"
  echo "--- nvidia-smi -L ---"
  nvidia-smi -L 2>&1 || echo "(no nvidia-smi)"
  echo "--- nvidia-smi query ---"
  nvidia-smi --query-gpu=index,name,driver_version,compute_cap,memory.total,power.limit --format=csv,noheader 2>&1 || true
  echo "--- detect_gpu_backend ---"
  HACKME_REPO_ROOT="$PACK_ROOT" bash "$PACK_ROOT/scripts/detect_gpu_backend.sh" 2>&1 || true
  echo "--- ldd workerpoh-cuda ---"
  ldd "$PACK_ROOT/bin/workerpoh-cuda" 2>&1 | head -40 || true
  echo "--- coordinator ping ---"
  if [[ -f "$PACK_ROOT/env.vast" ]]; then
    # shellcheck disable=SC1091
    source "$PACK_ROOT/env.vast"
  fi
  curl -fsS --max-time 15 "${COORD_URL:-https://hackme.tech/pool/coordinator}/api/work/stats" | head -c 400 || echo "coord unreachable"
  echo ""
} | tee "$OUT"

echo "[inventory] -> $OUT"
