#!/usr/bin/env bash
set -euo pipefail

# Universal worker launcher:
# - builds workerpoh if needed
# - auto-selects GPU backend/device for broad compatibility
# - runs in restart loop with bounded backoff
#
# Required env:
#   COORD_URL, COORD_TOKEN, HACKME_MINER_ED25519_SEED_HEX
# Optional env:
#   WORKER_ID, BATCH_SIZE, GPU_CHUNK, SEARCH_TIMEOUT_MS
#   HACKME_GPU_BACKEND=opencl|cuda|auto
#   HACKME_GPU_DEVICE=<int>
#   HACKME_GPU_DISABLE=1
#   HACKME_GPU_FLEET=1 (default) — one worker per GPU (up to HACKME_GPU_FLEET_MAX=20)
#   HACKME_GPU_HYBRID=auto — NVIDIA→CUDA + AMD→OpenCL on same host
#   HACKME_WORKER_HYBRID_FUZZ=1 — same worker_id also digs pool fuzz (default ON; set =0 to disable)
#   HACKME_WORKER_HYBRID_FUZZ_MODE=inline|process (default inline; process needs bin/workerfuzz)
#   HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY=1 (hard-capped at 2)
#   WORKER_BIN=/path/to/workerpoh
#   RESTART_MAX_BACKOFF_SEC=20

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-autostart] missing command: $1" >&2
    exit 1
  }
}

require_cmd awk

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_DIR="${ROOT_DIR}/logs"
mkdir -p "$LOG_DIR"

LOCK_FILE="${LOG_DIR}/.worker_autostart.lock"
exec 200>"$LOCK_FILE"
if ! flock -n 200; then
  echo "[worker-autostart] another instance is already running (lock ${LOCK_FILE}); exiting"
  exit 0
fi

mining_paused() {
  local f
  for f in "${HACKME_MINING_PAUSED_FILE:-}" \
    "${ROOT_DIR}/logs/mining_paused" \
    "${ROOT_DIR}/logs/desktop/mining_paused"; do
    if [[ -n "$f" && -f "$f" ]]; then
      return 0
    fi
  done
  return 1
}
if mining_paused && [[ "${FORCE_MINING:-0}" != "1" ]]; then
  echo "[worker-autostart] mining paused ($(ls "${ROOT_DIR}/logs/desktop/mining_paused" "${ROOT_DIR}/logs/mining_paused" 2>/dev/null | head -1)); exiting"
  exit 0
fi

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
COORD_TOKEN="${COORD_TOKEN:-${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}}"
WORKER_ID="${WORKER_ID:-worker-$(hostname -s 2>/dev/null || echo local)}"
RESTART_MAX_BACKOFF_SEC="${RESTART_MAX_BACKOFF_SEC:-20}"

WORKER_ENV_FILE="${WORKER_ENV_FILE:-${ROOT_DIR}/.env.worker}"
if [[ -f "$WORKER_ENV_FILE" ]]; then
	set -a
	# shellcheck disable=SC1090
	source "$WORKER_ENV_FILE"
	set +a
fi
# Desktop hybrid flags (gitignored .env.desktop) — do not override already-exported values.
if [[ -f "${ROOT_DIR}/.env.desktop" ]]; then
  while IFS='=' read -r k v; do
    [[ "$k" == HACKME_WORKER_HYBRID_FUZZ* ]] || continue
    if [[ -z "${!k:-}" ]]; then
      export "${k}=${v}"
    fi
  done < <(grep -E '^HACKME_WORKER_HYBRID_FUZZ' "${ROOT_DIR}/.env.desktop" || true)
fi

coord_looks_remote() {
  local u
  u="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ -z "$u" ]] && return 1
  [[ "$u" == *127.0.0.1* || "$u" == *localhost* || "$u" == *"::1"* ]] && return 1
  return 0
}

coord_is_public_cf() {
  local u
  u="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ "$u" == *hackme.tech* ]]
}

# Direct origin for GPU fleet (bypass CF). Opt-in: HACKME_POOL_DIRECT=1, or desktop GPU pool
# still on public CF path. Never rewrite loopback or custom non-CF remotes.
DIRECT_COORD_URL="${HACKME_POOL_DIRECT_URL:-http://132.243.112.100:18083}"
if [[ "${HACKME_POOL_DIRECT:-0}" == "1" ]] || [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]]; then
  cur_coord="${HACKME_POOL_COORDINATOR_URL:-${COORD_URL:-}}"
  if coord_looks_remote "$cur_coord"; then
    rewrite=0
    if [[ "${HACKME_POOL_DIRECT:-0}" == "1" ]]; then
      rewrite=1
    elif coord_is_public_cf "$cur_coord"; then
      # Desktop GPU + CF path → direct (no explicit non-CF override).
      rewrite=1
    fi
    if [[ "$rewrite" == "1" ]] && [[ "$cur_coord" != "$DIRECT_COORD_URL" ]]; then
      echo "[worker-autostart] coordinator → direct ${DIRECT_COORD_URL} (custom COORD_URL / unset HACKME_POOL_DIRECT to keep CF)"
      export COORD_URL="$DIRECT_COORD_URL"
      export HACKME_POOL_COORDINATOR_URL="$DIRECT_COORD_URL"
    fi
  fi
fi

# GPU desktop / CUDA: default 16M batch (fewer RTTs). Casual CF remote without GPU flags stays 1M.
if [[ -z "${BATCH_SIZE:-}" ]]; then
  if [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]] || [[ "${HACKME_GPU_BACKEND:-}" == "cuda" ]]; then
    BATCH_SIZE=16777216
    GPU_CHUNK="${GPU_CHUNK:-4194304}"
  elif coord_looks_remote "$COORD_URL"; then
    BATCH_SIZE=1048576
    GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
  else
    BATCH_SIZE=4194304
    GPU_CHUNK="${GPU_CHUNK:-4194304}"
  fi
fi
BATCH_SIZE="${BATCH_SIZE}"
GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
if coord_looks_remote "$COORD_URL"; then
  export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-90s}"
  export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-120s}"
else
  export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-35s}"
  export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-90s}"
fi
if [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]]; then
  SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-12000}"
else
  SEARCH_TIMEOUT_MS="${SEARCH_TIMEOUT_MS:-2500}"
fi
if [[ -n "${HACKME_WORKER_HASHRATE_GHS:-}" ]]; then
  export HASHRATE_GHS="${HACKME_WORKER_HASHRATE_GHS}"
else
  unset HASHRATE_GHS 2>/dev/null || true
fi

# Fast CUDA desktops with CLAIM_COOLDOWN_MS=0 hammer claim/submit → 429 bans + idle GPU.
# Floor at 100ms unless operator explicitly sets HACKME_WORKER_ALLOW_ZERO_COOLDOWN=1.
if [[ "${HACKME_WORKER_CLAIM_COOLDOWN_MS:-}" == "0" ]] && [[ "${HACKME_WORKER_ALLOW_ZERO_COOLDOWN:-0}" != "1" ]]; then
  if [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]] || [[ "${HACKME_GPU_BACKEND:-}" == "cuda" ]]; then
    echo "[worker-autostart] CLAIM_COOLDOWN_MS=0 → 100 (set HACKME_WORKER_ALLOW_ZERO_COOLDOWN=1 to keep 0)"
    export HACKME_WORKER_CLAIM_COOLDOWN_MS=100
  fi
fi
# Unset cooldown on GPU desktop/CUDA → bake 100 (matches workerpoh GPU default).
if [[ -z "${HACKME_WORKER_CLAIM_COOLDOWN_MS:-}" ]]; then
  if [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]] || [[ "${HACKME_GPU_BACKEND:-}" == "cuda" ]]; then
    export HACKME_WORKER_CLAIM_COOLDOWN_MS=100
  fi
fi

if [[ -z "${COORD_TOKEN}" ]]; then
  echo "[worker-autostart] set COORD_TOKEN (or COORD_ADMIN_TOKEN/ADMIN_TOKEN)" >&2
  exit 1
fi
if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
  echo "[worker-autostart] set HACKME_MINER_ED25519_SEED_HEX (64 hex chars)" >&2
  exit 1
fi

truthy() {
  local v
  v="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ "$v" == "1" || "$v" == "true" || "$v" == "yes" || "$v" == "on" ]]
}

detect_gpu_backend() {
  export HACKME_REPO_ROOT="${HACKME_REPO_ROOT:-$ROOT_DIR}"
  if [[ -x "${ROOT_DIR}/scripts/ops/detect_gpu_backend.sh" ]]; then
    bash "${ROOT_DIR}/scripts/ops/detect_gpu_backend.sh"
    return 0
  fi
  if truthy "${HACKME_GPU_DISABLE:-0}"; then
    echo "cpu"
    return 0
  fi
  if [[ -n "${HACKME_GPU_BACKEND:-}" && "${HACKME_GPU_BACKEND}" != "auto" ]]; then
    local req
    req="$(printf '%s' "${HACKME_GPU_BACKEND}" | tr '[:upper:]' '[:lower:]')"
    if [[ "$req" == "cuda" ]]; then
      if worker_bin_candidate workerpoh-cuda >/dev/null; then
        echo cuda
        return 0
      fi
      echo "[worker-autostart] WARN: HACKME_GPU_BACKEND=cuda but workerpoh-cuda missing — falling back to detect" >&2
    elif [[ "$req" == "opencl" || "$req" == "cpu" ]]; then
      echo "$req"
      return 0
    else
      echo "${HACKME_GPU_BACKEND}"
      return 0
    fi
  fi
  echo "cpu"
}

# Prefer release-layout paths: bin/X, then ./X (linux tarball root).
worker_bin_candidate() {
	local name="$1"
	local p
	for p in "${ROOT_DIR}/bin/${name}" "${ROOT_DIR}/${name}"; do
		if [[ -x "$p" ]]; then
			printf '%s\n' "$p"
			return 0
		fi
	done
	return 1
}

choose_worker_bin() {
	local backend="${1:-cpu}"
	local p=""
	if [[ -n "${WORKER_BIN:-}" ]]; then
		printf '%s\n' "$WORKER_BIN"
		return 0
	fi
	case "$backend" in
	cuda)
		if p="$(worker_bin_candidate workerpoh-cuda)"; then
			printf '%s\n' "$p"
			return 0
		fi
		# Release bundles ship static workerpoh; never invent workerpoh-cpu for cuda.
		if p="$(worker_bin_candidate workerpoh)"; then
			echo "[worker-autostart] WARN: backend=cuda but workerpoh-cuda missing — using ${p} (CPU/OpenCL flags only)" >&2
			printf '%s\n' "$p"
			return 0
		fi
		;;
	opencl)
		if p="$(worker_bin_candidate workerpoh-opencl)"; then
			printf '%s\n' "$p"
			return 0
		fi
		if p="$(worker_bin_candidate workerpoh)"; then
			echo "[worker-autostart] WARN: backend=opencl but workerpoh-opencl missing — using ${p}" >&2
			printf '%s\n' "$p"
			return 0
		fi
		;;
	cpu|*)
		if p="$(worker_bin_candidate workerpoh-cpu)"; then
			printf '%s\n' "$p"
			return 0
		fi
		if p="$(worker_bin_candidate workerpoh)"; then
			printf '%s\n' "$p"
			return 0
		fi
		;;
	esac
	echo "[worker-autostart] ERROR: no worker binary for backend=${backend} under ${ROOT_DIR}/bin or ${ROOT_DIR}" >&2
	return 1
}

# Release tarballs have no Go sources — never attempt go build there.
release_layout_no_sources() {
	[[ -x "${ROOT_DIR}/bin/workerpoh" || -x "${ROOT_DIR}/workerpoh" ]] || return 1
	[[ ! -f "${ROOT_DIR}/cmd/workerpoh/main.go" ]]
}

build_worker_if_needed() {
	local bin="$1"
	local backend="$2"
	if [[ -x "$bin" ]]; then
		return 0
	fi
	if release_layout_no_sources; then
		echo "[worker-autostart] ERROR: missing ${bin} (backend=${backend})." >&2
		echo "[worker-autostart] Linux release ships bin/workerpoh[+cuda|+opencl]; do not require go build." >&2
		echo "[worker-autostart] Fix: re-download bundle, or set WORKER_BIN=/path/to/workerpoh, or HACKME_GPU_BACKEND=cpu." >&2
		return 1
	fi
	if truthy "${SKIP_WORKER_BUILD:-0}"; then
		echo "[worker-autostart] ERROR: missing ${bin} and SKIP_WORKER_BUILD=1" >&2
		return 1
	fi
	mkdir -p "$(dirname "$bin")"
	export GOCACHE="${GOCACHE:-${ROOT_DIR}/.cache/go-build}"
	mkdir -p "$GOCACHE" 2>/dev/null || true
	echo "[worker-autostart] building worker binary: ${bin}"
	if [[ "$backend" == "cuda" ]]; then
		if [[ -x "$ROOT_DIR/scripts/ops/build_cuda_worker.sh" ]]; then
			OUT_CUDA="${ROOT_DIR}/bin/workerpoh-cuda"
			bash "$ROOT_DIR/scripts/ops/build_cuda_worker.sh"
			cp -f "$OUT_CUDA" "$bin" 2>/dev/null || ln -sf "$OUT_CUDA" "$bin"
		else
			(cd "$ROOT_DIR" && go build -tags "cuda,opencl" -o "$bin" ./cmd/workerpoh)
		fi
	elif [[ "$backend" == "opencl" ]]; then
		(cd "$ROOT_DIR" && go build -tags opencl -o "$bin" ./cmd/workerpoh)
	else
		(cd "$ROOT_DIR" && go build -o "$bin" ./cmd/workerpoh)
	fi
}

# Bundled NVRTC (linux/lib) for workerpoh-cuda without system CUDA toolkit.
export_cuda_lib_path() {
	local libdir=""
	for libdir in "${ROOT_DIR}/lib" "${ROOT_DIR}/lib/cuda" "${ROOT_DIR}/.deps/cuda-lib"; do
		if [[ -e "${libdir}/libnvrtc.so.12" || -e "${libdir}/libnvrtc.so" ]]; then
			export LD_LIBRARY_PATH="${libdir}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
			return 0
		fi
	done
	return 0
}

load_fleet_plan_json() {
  local fp="${ROOT_DIR}/bin/fleetplan"
  if [[ ! -x "$fp" ]]; then
    if command -v go >/dev/null 2>&1; then
      (cd "$ROOT_DIR" && go build -o "$fp" ./cmd/fleetplan) 2>/dev/null || true
    fi
  fi
  if [[ ! -x "$fp" ]]; then
    return 1
  fi
  local out
  out="$(HACKME_REPO_ROOT="$ROOT_DIR" "$fp" -repo "$ROOT_DIR" -worker "$WORKER_ID" 2>/dev/null)" || return 1
  if [[ -z "$out" ]]; then
    return 1
  fi
  if ! echo "$out" | python3 -c "import json,sys; json.load(sys.stdin)" 2>/dev/null; then
    return 1
  fi
  printf '%s\n' "$out"
  return 0
}

worker_run_loop_slot() {
  local worker_id="$1"
  local slot_backend="$2"
  local gpu_dev="${3:-}"
  local slot_batch="${4:-$BATCH_SIZE}"
  local slot_chunk="${5:-$GPU_CHUNK}"
  local slot_timeout="${6:-$SEARCH_TIMEOUT_MS}"
  local slot_bin
  if ! slot_bin="$(choose_worker_bin "$slot_backend")"; then
    echo "[worker-autostart] FATAL: cannot choose worker binary for backend=${slot_backend}" >&2
    return 1
  fi
  export_cuda_lib_path
  if ! build_worker_if_needed "$slot_bin" "$slot_backend"; then
    echo "[worker-autostart] FATAL: worker binary unavailable: ${slot_bin}" >&2
    return 1
  fi
  # If cuda was requested but we fell back to plain workerpoh, don't pass -gpu-backend cuda.
  if [[ "$slot_backend" == "cuda" && "$slot_bin" != *workerpoh-cuda* ]]; then
    slot_backend="cpu"
  fi
  if [[ "$slot_backend" == "opencl" && "$slot_bin" != *workerpoh-opencl* && "$slot_bin" != *workerpoh-cuda* ]]; then
    # plain static workerpoh may still accept -gpu-backend opencl if built with tags; otherwise cpu.
    if ! "$slot_bin" -h 2>&1 | grep -q -- '-gpu-backend'; then
      slot_backend="cpu"
    fi
  fi
  local bin_help="$("$slot_bin" -h 2>&1 || true)"
  supports_flag() { [[ "$bin_help" == *"$1"* ]]; }
  local backend_flag=() dev_flag=() disable_flag=()
  if [[ "$slot_backend" == "cpu" ]]; then
    supports_flag "-gpu-disable" && disable_flag=(-gpu-disable)
  else
    supports_flag "-gpu-backend" && backend_flag=(-gpu-backend "$slot_backend")
  fi
  if [[ -n "$gpu_dev" ]] && supports_flag "-gpu-device"; then
    dev_flag=(-gpu-device "$gpu_dev")
  fi
  local backoff=1
  while true; do
    ts="$(date +%Y%m%dT%H%M%S)"
    run_log="${LOG_DIR}/workerpoh-${worker_id}-${ts}.log"
    echo "[worker-autostart] launch worker=${worker_id} backend=${slot_backend} bin=${slot_bin} device=${gpu_dev:-auto} batch=${slot_batch} log=${run_log}"
    set +e
    "${slot_bin}" \
      -coord "${COORD_URL}" \
      -token "${COORD_TOKEN}" \
      -worker "${worker_id}" \
      -batch "${slot_batch}" \
      -gpu-chunk "${slot_chunk}" \
      -search-timeout-ms "${slot_timeout}" \
      "${backend_flag[@]}" \
      "${dev_flag[@]}" \
      "${disable_flag[@]}" \
      2>&1 | tee -a "${run_log}"
    rc="${PIPESTATUS[0]}"
    set -e
    echo "[worker-autostart] worker=${worker_id} exited rc=${rc}; restart in ${backoff}s"
    sleep "${backoff}"
    if (( backoff < RESTART_MAX_BACKOFF_SEC )); then
      backoff=$((backoff * 2))
      if (( backoff > RESTART_MAX_BACKOFF_SEC )); then
        backoff="${RESTART_MAX_BACKOFF_SEC}"
      fi
    fi
  done
}

# Supervised fuzz dig under the same WORKER_ID (process mode / pre-hybrid CUDA binaries).
# Inline mode is handled inside workerpoh when the binary includes hybrid_fuzz.go.
hybrid_fuzz_process_loop() {
  local worker_id="$1"
  local fuzz_bin="${HACKME_WORKERFUZZ_BIN:-${ROOT_DIR}/bin/workerfuzz}"
  if [[ ! -x "$fuzz_bin" ]]; then
    if command -v go >/dev/null 2>&1; then
      echo "[worker-autostart] building workerfuzz for hybrid process mode"
      (cd "$ROOT_DIR" && go build -o "$fuzz_bin" ./cmd/workerfuzz) || {
        echo "[worker-autostart] hybrid fuzz: build workerfuzz failed" >&2
        return 1
      }
    else
      echo "[worker-autostart] hybrid fuzz: missing $fuzz_bin" >&2
      return 1
    fi
  fi
  local timeout_ms="${HACKME_WORKER_HYBRID_FUZZ_TIMEOUT_MS:-500}"
  local backoff=2
  local run_log="${LOG_DIR}/workerpoh-hybrid-fuzz-${worker_id}.log"
  echo "[worker-autostart] hybrid fuzz process mode worker=${worker_id} bin=${fuzz_bin} log=${run_log}"
  while true; do
    set +e
    nice -n "${HACKME_WORKER_HYBRID_FUZZ_NICE:-10}" \
      "$fuzz_bin" \
      -coord "${COORD_URL}" \
      -token "${COORD_TOKEN}" \
      -worker "${worker_id}" \
      -timeout-ms "${timeout_ms}" \
      >>"${run_log}" 2>&1
    rc=$?
    set -e
    echo "[worker-autostart] hybrid fuzz exited rc=${rc}; restart in ${backoff}s" | tee -a "${run_log}"
    sleep "${backoff}"
    if (( backoff < 30 )); then
      backoff=$((backoff * 2))
    fi
  done
}

start_hybrid_fuzz_if_needed() {
  # Match Go default: hybrid ON unless explicitly off.
  if [[ -n "${HACKME_WORKER_HYBRID_FUZZ:-}" ]] && ! truthy "${HACKME_WORKER_HYBRID_FUZZ}"; then
    echo "[worker-autostart] hybrid fuzz disabled (HACKME_WORKER_HYBRID_FUZZ=${HACKME_WORKER_HYBRID_FUZZ})"
    return 0
  fi
  local mode
  mode="$(printf '%s' "${HACKME_WORKER_HYBRID_FUZZ_MODE:-inline}" | tr '[:upper:]' '[:lower:]')"
  # Inline is handled inside workerpoh; process mode supervises bin/workerfuzz.
  if [[ "$mode" == "inline" || "$mode" == "" || "$mode" == "default" ]]; then
    echo "[worker-autostart] hybrid fuzz inline mode (default) — workerpoh digs fuzz under same worker_id"
    return 0
  fi
  if [[ "$mode" != "process" ]]; then
    echo "[worker-autostart] WARN: unknown HACKME_WORKER_HYBRID_FUZZ_MODE=${mode}; treating as inline" >&2
    return 0
  fi
  # Avoid double dig when an inline workerpoh is already running for this worker_id.
  if pgrep -f "${ROOT_DIR}/bin/workerpoh.*-worker[ =]${WORKER_ID}( |$)" >/dev/null 2>&1 || \
     pgrep -f "workerpoh-cuda.*-worker[ =]${WORKER_ID}( |$)" >/dev/null 2>&1; then
    echo "[worker-autostart] hybrid process skipped — workerpoh already running for ${WORKER_ID} (use MODE=inline or stop PoH first)"
    return 0
  fi
  hybrid_fuzz_process_loop "${WORKER_ID}" &
  HYBRID_FUZZ_PID=$!
  echo "[worker-autostart] hybrid fuzz pid=${HYBRID_FUZZ_PID} (same worker_id=${WORKER_ID})"
}

plan_json=""
if plan_json="$(load_fleet_plan_json)"; then
  :
else
  backend="$(detect_gpu_backend)"
  plan_json="$(python3 - "$ROOT_DIR" "$WORKER_ID" "$backend" <<'PY' 2>/dev/null || true
import json, os, subprocess, sys
root, wid, backend = sys.argv[1:4]
fp = os.path.join(root, "bin", "fleetplan")
if os.path.isfile(fp) and os.access(fp, os.X_OK):
    import os as O
    env = dict(O.environ)
    env["HACKME_REPO_ROOT"] = root
    out = subprocess.check_output([fp, "-repo", root, "-worker", wid], env=env, text=True)
    print(out.strip())
else:
    print(json.dumps({"total_slots":1,"slots":[{"worker_suffix":"","backend":backend,"device_index":0}]}))
PY
)"
fi

if [[ -z "$plan_json" ]]; then
  backend="$(detect_gpu_backend)"
  echo "[worker-autostart] WARN: fleet plan unavailable; single worker backend=${backend}" >&2
  start_hybrid_fuzz_if_needed
  worker_run_loop_slot "${WORKER_ID}" "$backend" "${HACKME_GPU_DEVICE:-}" &
  wait
  exit 0
fi

echo "[worker-autostart] coord=${COORD_URL} base_worker=${WORKER_ID}"
start_hybrid_fuzz_if_needed
echo "[worker-autostart] fleet plan: $(printf '%s' "$plan_json" | python3 -c "
import json, sys
raw = sys.stdin.read().strip()
if not raw:
    print('?')
else:
    try:
        p = json.loads(raw)
        print('hybrid=%s slots=%s' % (p.get('hybrid'), p.get('total_slots')))
    except json.JSONDecodeError:
        print('? (invalid json)')
" 2>/dev/null || echo '?')"

fleet_pids=()
while IFS= read -r slot_line; do
  [[ -n "$slot_line" ]] || continue
  eval "$(python3 - "$slot_line" <<'PY'
import json, shlex, sys
s = json.loads(sys.argv[1])
wid = s.get("worker_id","")
backend = s.get("backend","cpu")
dev = s.get("device_index",0)
batch = s.get("batch","")
chunk = s.get("chunk","")
timeout = s.get("timeout","")
env_exports = s.get("env_exports","")
print("slot_worker_id=" + shlex.quote(wid))
print("slot_backend=" + shlex.quote(backend))
print("slot_dev=" + shlex.quote(str(dev)))
print("slot_batch=" + shlex.quote(batch))
print("slot_chunk=" + shlex.quote(chunk))
print("slot_timeout=" + shlex.quote(timeout))
print("slot_env_exports=" + shlex.quote(env_exports))
PY
)"
  # shellcheck disable=SC2086
  (
    if [[ -n "${slot_env_exports:-}" ]]; then
      eval "$slot_env_exports"
    fi
    # Fleet/rig profile may re-inject CLAIM_COOLDOWN_MS=0 / small batch after the global floor above.
    if [[ "${HACKME_WORKER_CLAIM_COOLDOWN_MS:-}" == "0" ]] && [[ "${HACKME_WORKER_ALLOW_ZERO_COOLDOWN:-0}" != "1" ]]; then
      if [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]] || [[ "${slot_backend}" == "cuda" ]]; then
        echo "[worker-autostart] slot CLAIM_COOLDOWN_MS=0 → 100"
        export HACKME_WORKER_CLAIM_COOLDOWN_MS=100
      fi
    fi
    # Prefer large PoH batches on fast CUDA desktops (fewer HTTPS RTTs per attempt). Cap by env override.
    if [[ "${slot_backend}" == "cuda" ]] || [[ "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]]; then
      want_batch="${HACKME_WORKER_BATCH_SIZE_FORCE:-16777216}"
      cur_batch="${slot_batch:-${BATCH_SIZE:-0}}"
      if [[ "$cur_batch" =~ ^[0-9]+$ ]] && (( cur_batch < want_batch )); then
        echo "[worker-autostart] slot batch ${cur_batch} → ${want_batch} (fewer claim RTTs)"
        slot_batch="$want_batch"
        export BATCH_SIZE="$want_batch"
        export HACKME_WORKER_BATCH_SIZE="$want_batch"
      fi
    fi
    if [[ "$slot_backend" == "opencl" ]]; then
      export HACKME_FORCE_OPENCL=1
      export HACKME_GPU_BACKEND=opencl
    fi
    if [[ "$slot_backend" == "cuda" && -x "${ROOT_DIR}/bin/workerpoh-cuda" ]]; then
      export WORKER_BIN="${ROOT_DIR}/bin/workerpoh-cuda"
    fi
    worker_run_loop_slot "$slot_worker_id" "$slot_backend" "$slot_dev" "${slot_batch:-$BATCH_SIZE}" "${slot_chunk:-$GPU_CHUNK}" "${slot_timeout:-$SEARCH_TIMEOUT_MS}"
  ) &
  fleet_pids+=("$!")
done < <(PLAN_JSON="$plan_json" python3 - "$WORKER_ID" "$BATCH_SIZE" "$GPU_CHUNK" "$SEARCH_TIMEOUT_MS" <<'PY'
import json, os, shlex, sys
plan = json.loads(os.environ["PLAN_JSON"])
base = sys.argv[1]
def_batch = sys.argv[2]
def_chunk = sys.argv[3]
def_timeout = sys.argv[4]
root = os.environ.get("HACKME_REPO_ROOT") or os.getcwd()
for slot in plan.get("slots") or []:
    sfx = slot.get("worker_suffix") or ""
    # Always keep -gpuN in submit worker_id; coordinator merges to base when
    # HACKME_GPU_FLEET_AGGREGATE_ID=1 (fleetBaseWorkerID + mergeWorkerStat).
    wid = base + sfx
    env = slot.get("env") or {}
    batch = env.get("HACKME_WORKER_BATCH_SIZE") or def_batch
    chunk = env.get("GPU_CHUNK") or env.get("HACKME_WORKER_BATCH_SIZE") or def_chunk
    timeout = env.get("SEARCH_TIMEOUT_MS") or def_timeout
    exports = []
    # Ensure each slot has a dedicated nonce stream to avoid replay collisions,
    # including aggregate-id mode where all GPUs share one worker_id.
    if "HACKME_MINER_NONCE_FILE" not in env:
        safe = "".join(ch if ch.isalnum() or ch in ("-","_") else "_" for ch in base)
        env["HACKME_MINER_NONCE_FILE"] = f"{root}/logs/miner_submit_nonce.{safe}.gpu{int(slot.get('device_index') or 0)}.seq"
    for k, v in env.items():
        if v is None:
            continue
        exports.append(f"export {k}={shlex.quote(str(v))}")
    row = {
        "worker_id": wid,
        "backend": slot.get("backend") or "cpu",
        "device_index": int(slot.get("device_index") or 0),
        "batch": str(batch),
        "chunk": str(chunk),
        "timeout": str(timeout),
        "env_exports": "; ".join(exports),
    }
    print(json.dumps(row))
PY
)

if [[ ${#fleet_pids[@]} -eq 0 ]]; then
  backend="$(detect_gpu_backend)"
  worker_run_loop_slot "${WORKER_ID}" "$backend" "${HACKME_GPU_DEVICE:-}" &
  fleet_pids=("$!")
fi

if [[ -n "${HYBRID_FUZZ_PID:-}" ]]; then
  fleet_pids+=("${HYBRID_FUZZ_PID}")
fi

echo "[worker-autostart] fleet workers=${#fleet_pids[@]} (max ${HACKME_GPU_FLEET_MAX:-20})"
wait "${fleet_pids[@]}"

