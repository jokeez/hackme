#!/usr/bin/env bash
# Apply HackMe rig profile env (same tuning as dashboard / Windows RX580 ideal).
set -euo pipefail

RIG_ENV="/var/lib/hackme/rig.env"
mkdir -p /var/lib/hackme

gpu_blob="$(
  (lspci 2>/dev/null; clinfo 2>/dev/null | grep -E 'Device Name|Board name' || true) \
    | tr '[:upper:]' '[:lower:]' | tr -s ' \n' ' '
)"

profile=""
if [[ "$gpu_blob" == *"rx 580"* && "$gpu_blob" == *"2048"* ]]; then
  profile="amd_rx580_2048sp"
elif [[ "$gpu_blob" == *"rx 580"* ]]; then
  profile="amd_rx580_generic"
elif [[ "$gpu_blob" == *"rx 5"* || "$gpu_blob" == *"rx 6"* || "$gpu_blob" == *"polaris"* ]]; then
  profile="amd_rx580_generic"
elif [[ "$gpu_blob" == *"arc"* ]]; then
  profile="intel_arc_daily"
elif [[ "$gpu_blob" == *"nvidia"* || "$gpu_blob" == *"geforce"* || "$gpu_blob" == *"rtx"* ]]; then
  profile="nvidia_cuda_daily"
else
  profile="generic_opencl"
fi

# Env maps mirror internal/gputune/rig_profiles.go (pool-optimized batches).
case "$profile" in
  amd_rx580_2048sp)
    cat >"$RIG_ENV" <<'EOF'
HACKME_RIG_PROFILE=amd_rx580_2048sp
HACKME_GPU_BACKEND=opencl
HACKME_WORKER_BATCH_SIZE=1048576
BATCH_SIZE=1048576
GPU_CHUNK=524288
SEARCH_TIMEOUT_MS=4500
HACKME_WORKER_CLAIM_COOLDOWN_MS=28000
HACKME_GPU_TEMP_PAUSE_C=78
HACKME_GPU_TEMP_RESUME_C=72
HACKME_DESKTOP_GPU_POOL=1
EOF
    ;;
  amd_rx580_generic)
    cat >"$RIG_ENV" <<'EOF'
HACKME_RIG_PROFILE=amd_rx580_generic
HACKME_GPU_BACKEND=opencl
HACKME_WORKER_BATCH_SIZE=2097152
BATCH_SIZE=2097152
GPU_CHUNK=1048576
SEARCH_TIMEOUT_MS=4000
HACKME_WORKER_CLAIM_COOLDOWN_MS=28000
HACKME_GPU_TEMP_PAUSE_C=80
HACKME_GPU_TEMP_RESUME_C=74
HACKME_DESKTOP_GPU_POOL=1
EOF
    ;;
  nvidia_cuda_daily)
    cat >"$RIG_ENV" <<'EOF'
HACKME_RIG_PROFILE=nvidia_cuda_daily
HACKME_GPU_BACKEND=cuda
HACKME_WORKER_BATCH_SIZE=16777216
BATCH_SIZE=16777216
GPU_CHUNK=4194304
SEARCH_TIMEOUT_MS=12000
HACKME_WORKER_CLAIM_COOLDOWN_MS=100
HACKME_DESKTOP_GPU_POOL=1
EOF
    ;;
  intel_arc_daily)
    cat >"$RIG_ENV" <<'EOF'
HACKME_RIG_PROFILE=intel_arc_daily
HACKME_GPU_BACKEND=opencl
HACKME_WORKER_BATCH_SIZE=2097152
BATCH_SIZE=2097152
GPU_CHUNK=1048576
SEARCH_TIMEOUT_MS=5000
HACKME_WORKER_CLAIM_COOLDOWN_MS=100
HACKME_DESKTOP_GPU_POOL=1
EOF
    ;;
  *)
    cat >"$RIG_ENV" <<'EOF'
HACKME_RIG_PROFILE=generic_opencl
HACKME_GPU_BACKEND=auto
HACKME_WORKER_BATCH_SIZE=16777216
BATCH_SIZE=16777216
GPU_CHUNK=4194304
SEARCH_TIMEOUT_MS=12000
HACKME_WORKER_CLAIM_COOLDOWN_MS=100
HACKME_DESKTOP_GPU_POOL=1
EOF
    ;;
esac
chmod 600 "$RIG_ENV"
echo "[hackme-os-rig] profile=${profile} → ${RIG_ENV}"
