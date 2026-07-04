#!/usr/bin/env bash
# Build Linux GPU workers + pack everything for Vast.ai matrix testing (upload one tarball).
#
#   bash scripts/ops/pack_vast_gpu_matrix.sh
#   bash scripts/ops/pack_vast_gpu_matrix.sh --skip-build   # repack only
#
# Output: dist/vast-gpu-matrix-<stamp>/ and dist/vast-gpu-matrix-<stamp>.tar.gz
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

SKIP_BUILD=0
INCLUDE_TOKEN=0
for arg in "$@"; do
  case "$arg" in
    --skip-build) SKIP_BUILD=1 ;;
    --include-token) INCLUDE_TOKEN=1 ;;
  esac
done

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
PACK_DIR="$ROOT/dist/vast-gpu-matrix-$STAMP"
ARCHIVE="$ROOT/dist/vast-gpu-matrix-$STAMP.tar.gz"

rm -rf "$PACK_DIR"
mkdir -p "$PACK_DIR"/{bin,scripts,docs,reports,.secrets}

echo "[vast-pack] stamp=$STAMP"

if [[ "$SKIP_BUILD" != "1" ]]; then
  echo "[vast-pack] building GPU workers (native — needs CUDA toolkit on this host for workerpoh-cuda)"
  bash "$ROOT/scripts/ops/build_gpu_workers.sh" || {
    echo "[vast-pack] WARN: build_gpu_workers failed; packing existing bin/ if present" >&2
  }
fi
if [[ ! -x "$PACK_DIR/bin/minersign" || ! -x "$PACK_DIR/bin/fleetplan" ]]; then
  echo "[vast-pack] building minersign + fleetplan (linux amd64)"
  export GOCACHE="${GOCACHE:-$ROOT/.cache/go-build}"
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$PACK_DIR/bin/minersign" ./cmd/minersign
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$PACK_DIR/bin/fleetplan" ./cmd/fleetplan
  chmod 755 "$PACK_DIR/bin/minersign" "$PACK_DIR/bin/fleetplan"
fi

for b in workerpoh-cuda workerpoh-opencl workerpoh; do
  if [[ -x "$ROOT/bin/$b" ]]; then
    cp -f "$ROOT/bin/$b" "$PACK_DIR/bin/"
    echo "[vast-pack] + bin/$b"
  fi
done
if [[ ! -x "$PACK_DIR/bin/workerpoh-cuda" ]]; then
  echo "[vast-pack] ERROR: bin/workerpoh-cuda missing — run: bash scripts/ops/build_cuda_worker.sh" >&2
  exit 1
fi

cp -f "$ROOT/scripts/ops/detect_gpu_backend.sh" "$PACK_DIR/scripts/"
cp -f "$ROOT/scripts/ops/worker_autostart.sh" "$PACK_DIR/scripts/"
for f in "$ROOT"/scripts/vast/*.sh; do
  [[ -f "$f" ]] || continue
  cp -f "$f" "$PACK_DIR/scripts/"
done
chmod +x "$PACK_DIR/scripts/"*.sh

cp -f "$ROOT/docs/archive/vast/VAST_GPU_MATRIX_RUNBOOK.md" "$PACK_DIR/docs/" 2>/dev/null || true
cp -f "$ROOT/docs/archive/vast/VAST_GPU_DAY_PLAN.md" "$PACK_DIR/docs/" 2>/dev/null || true
cp -f "$ROOT/docs/archive/vast/VAST_GPU_FULL_MATRIX.md" "$PACK_DIR/docs/" 2>/dev/null || true
cp -f "$ROOT/docs/GPU_MINING_BACKENDS.md" "$PACK_DIR/docs/" 2>/dev/null || true

# Env template (no secrets in git)
cat >"$PACK_DIR/env.vast.example" <<'EOF'
# Copy to env.vast and fill before running on Vast:
#   cp env.vast.example env.vast && nano env.vast

COORD_URL=https://hackme.tech/pool/coordinator
# Worker token only (NOT node admin). From .secrets/hackme_coordinator_worker_token
COORD_TOKEN=

# Unique per Vast instance, e.g. vast-rtx4090-01
WORKER_ID=vast-gpu-01

# Payout address (optional; must be in WORKER_PAYOUT_MAP on hub if used)
# WALLET_HMC=HMC-xxxxxxxxxxxxxxxx

# 64 hex chars — generate once: ./bin/minersign -gen-seed
HACKME_MINER_ED25519_SEED_HEX=

# Pool tuning (NVIDIA remote)
HACKME_GPU_BACKEND=cuda
BATCH_SIZE=4194304
GPU_CHUNK=4194304
SEARCH_TIMEOUT_MS=12000
HACKME_WORKER_CLAIM_TIMEOUT=90s
HACKME_WORKER_SUBMIT_TIMEOUT=120s
HACKME_CUDA_VERBOSE=1

# Session length for run_pool_worker.sh (seconds)
RUN_SECONDS=1800
EOF

if [[ "$INCLUDE_TOKEN" == "1" && -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]]; then
  tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token" >"$PACK_DIR/.secrets/coordinator_worker_token"
  echo "[vast-pack] included worker token in pack (do not publish tarball)"
fi

if [[ -f "$ROOT/.env.vast" ]]; then
  cp -f "$ROOT/.env.vast" "$PACK_DIR/env.vast"
  echo "[vast-pack] included local .env.vast"
elif [[ -f "$PACK_DIR/.secrets/coordinator_worker_token" ]]; then
  tok="$(tr -d '\r\n' <"$PACK_DIR/.secrets/coordinator_worker_token")"
  sed "s/^COORD_TOKEN=.*/COORD_TOKEN=${tok}/" "$PACK_DIR/env.vast.example" >"$PACK_DIR/env.vast"
  echo "[vast-pack] wrote env.vast from packed token"
fi

if [[ -f "$PACK_DIR/env.vast" ]] && ! grep -q '^HACKME_MINER_ED25519_SEED_HEX=.\{32\}' "$PACK_DIR/env.vast" 2>/dev/null; then
  seed_json="$("$PACK_DIR/bin/minersign" -gen-seed 2>/dev/null || true)"
  seed_hex="$(echo "$seed_json" | sed -n 's/.*"HACKME_MINER_ED25519_SEED_HEX":"\([^"]*\)".*/\1/p')"
  addr="$(echo "$seed_json" | sed -n 's/.*"miner_address_from_pubkey":"\([^"]*\)".*/\1/p')"
  if [[ -n "$seed_hex" ]]; then
    if grep -q '^HACKME_MINER_ED25519_SEED_HEX=' "$PACK_DIR/env.vast"; then
      sed -i "s|^HACKME_MINER_ED25519_SEED_HEX=.*|HACKME_MINER_ED25519_SEED_HEX=${seed_hex}|" "$PACK_DIR/env.vast"
    else
      echo "HACKME_MINER_ED25519_SEED_HEX=${seed_hex}" >>"$PACK_DIR/env.vast"
    fi
    echo "[vast-pack] generated test miner seed -> payout ${addr:-?} (add to WORKER_PAYOUT_MAP on hub if needed)"
    echo "$addr" >"$PACK_DIR/VAST_TEST_PAYOUT_ADDRESS.txt"
  fi
fi

cp -f "$PACK_DIR/env.vast.example" "$ROOT/env.vast.example" 2>/dev/null || true

cat >"$PACK_DIR/GPU_MATRIX_SHEET.csv" <<'EOF'
instance_id,gpu_name,driver,compute_cap,vram_gb,backend,detect_backend,ghs_peak,ghs_avg_15m,pass,fail_reason,notes
EOF

cat >"$PACK_DIR/README.txt" <<EOF
HackMe Vast.ai GPU matrix pack — $STAMP

1. Upload $(basename "$ARCHIVE") to the instance (scp / Jupyter / vast sync).
2. tar xzf $(basename "$ARCHIVE") && cd vast-gpu-matrix-$STAMP
3. cp env.vast.example env.vast && edit COORD_TOKEN, WORKER_ID, HACKME_MINER_ED25519_SEED_HEX
4. bash scripts/00_inventory.sh
5. bash scripts/01_run_pool_worker.sh
6. bash scripts/03_ui_snapshot.sh
7. bash scripts/02_collect_report.sh  (copy reports/ back to your PC)
8. Multi-GPU: bash scripts/01_run_fleet.sh

Runbook: docs/VAST_GPU_MATRIX_RUNBOOK.md · Today: docs/VAST_GPU_DAY_PLAN.md
EOF

tar -czf "$ARCHIVE" -C "$ROOT/dist" "vast-gpu-matrix-$STAMP"
echo "[vast-pack] OK"
echo "[vast-pack] dir:  $PACK_DIR"
echo "[vast-pack] tar:  $ARCHIVE"
echo "[vast-pack] size: $(du -h "$ARCHIVE" | awk '{print $1}')"
ls -la "$PACK_DIR/bin/"
