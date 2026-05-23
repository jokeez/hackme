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
#   WORKER_BIN=/path/to/workerpoh
#   RESTART_MAX_BACKOFF_SEC=20

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker-autostart] missing command: $1" >&2
    exit 1
  }
}

require_cmd go
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

coord_looks_remote() {
  local u
  u="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')"
  [[ -z "$u" ]] && return 1
  [[ "$u" == *127.0.0.1* || "$u" == *localhost* || "$u" == *"::1"* ]] && return 1
  return 0
}

	if coord_looks_remote "$COORD_URL"; then
  # Desktop GPU rigs: larger batches (fewer HTTPS round-trips, more attempts per range).
  if [[ -z "${BATCH_SIZE:-}" && "${HACKME_DESKTOP_GPU_POOL:-0}" == "1" ]]; then
    BATCH_SIZE=4194304
    GPU_CHUNK=4194304
  fi
  BATCH_SIZE="${BATCH_SIZE:-1048576}"
  GPU_CHUNK="${GPU_CHUNK:-$BATCH_SIZE}"
  export HACKME_WORKER_CLAIM_TIMEOUT="${HACKME_WORKER_CLAIM_TIMEOUT:-90s}"
  export HACKME_WORKER_SUBMIT_TIMEOUT="${HACKME_WORKER_SUBMIT_TIMEOUT:-120s}"
else
  BATCH_SIZE="${BATCH_SIZE:-4194304}"
  GPU_CHUNK="${GPU_CHUNK:-4194304}"
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
    echo "${HACKME_GPU_BACKEND}"
    return 0
  fi
  echo "cpu"
}

choose_worker_bin() {
	local backend="${1:-cpu}"
	if [[ -n "${WORKER_BIN:-}" ]]; then
		printf '%s\n' "$WORKER_BIN"
		return 0
	fi
	if [[ "$backend" == "cuda" && -x "${ROOT_DIR}/bin/workerpoh-cuda" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-cuda"
		return 0
	fi
	if [[ "$backend" == "opencl" && -x "${ROOT_DIR}/bin/workerpoh-opencl" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-opencl"
		return 0
	fi
	if [[ "$backend" == "cpu" && -x "${ROOT_DIR}/bin/workerpoh-cpu" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh-cpu"
		return 0
	fi
	if [[ "$backend" == "cpu" && -x "${ROOT_DIR}/bin/workerpoh" ]]; then
		printf '%s\n' "${ROOT_DIR}/bin/workerpoh"
		return 0
	fi
	printf '%s\n' "${ROOT_DIR}/bin/workerpoh-cpu"
}

build_worker_if_needed() {
	local bin="$1"
	local backend="$2"
	if [[ -x "$bin" ]] && truthy "${SKIP_WORKER_BUILD:-0}"; then
		return 0
	fi
	mkdir -p "$(dirname "$bin")"
	export GOCACHE="${GOCACHE:-${ROOT_DIR}/.cache/go-build}"
	mkdir -p "$GOCACHE" 2>/dev/null || true
	if [[ -x "$bin" ]] && ! truthy "${FORCE_WORKER_REBUILD:-0}"; then
		return 0
	fi
	if [[ -x "$bin" ]]; then
		local src="${ROOT_DIR}/cmd/workerpoh/main.go"
		if [[ -f "$src" && "$bin" -nt "$src" ]]; then
			return 0
		fi
		echo "[worker-autostart] rebuilding stale worker binary: ${bin}"
	fi
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

load_fleet_plan_json() {
  local fp="${ROOT_DIR}/bin/fleetplan"
  if [[ ! -x "$fp" ]]; then
    if command -v go >/dev/null 2>&1; then
      (cd "$ROOT_DIR" && go build -o "$fp" ./cmd/fleetplan) 2>/dev/null || true
    fi
  fi
  if [[ -x "$fp" ]]; then
    HACKME_REPO_ROOT="$ROOT_DIR" "$fp" -repo "$ROOT_DIR" -worker "$WORKER_ID" 2>/dev/null || true
    return 0
  fi
  return 1
}

worker_run_loop_slot() {
  local worker_id="$1"
  local slot_backend="$2"
  local gpu_dev="${3:-}"
  local slot_batch="${4:-$BATCH_SIZE}"
  local slot_chunk="${5:-$GPU_CHUNK}"
  local slot_timeout="${6:-$SEARCH_TIMEOUT_MS}"
  local slot_bin
  slot_bin="$(choose_worker_bin "$slot_backend")"
  build_worker_if_needed "$slot_bin" "$slot_backend"
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
    echo "[worker-autostart] launch worker=${worker_id} backend=${slot_backend} device=${gpu_dev:-auto} batch=${slot_batch} log=${run_log}"
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
  worker_run_loop_slot "${WORKER_ID}" "$backend" "${HACKME_GPU_DEVICE:-}" &
  wait
  exit 0
fi

echo "[worker-autostart] coord=${COORD_URL} base_worker=${WORKER_ID}"
echo "[worker-autostart] fleet plan: $(echo "$plan_json" | python3 -c "import json,sys; p=json.load(sys.stdin); print('hybrid=%s slots=%s'%(p.get('hybrid'),p.get('total_slots')))" 2>/dev/null || echo '?')"

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
done < <(echo "$plan_json" | python3 - "$WORKER_ID" "$BATCH_SIZE" "$GPU_CHUNK" "$SEARCH_TIMEOUT_MS" <<'PY'
import json, os, shlex, sys
plan = json.load(sys.stdin)
base = sys.argv[1]
def_batch = sys.argv[2]
def_chunk = sys.argv[3]
def_timeout = sys.argv[4]
for slot in plan.get("slots") or []:
    sfx = slot.get("worker_suffix") or ""
    wid = base + sfx
    env = slot.get("env") or {}
    batch = env.get("HACKME_WORKER_BATCH_SIZE") or def_batch
    chunk = env.get("GPU_CHUNK") or env.get("HACKME_WORKER_BATCH_SIZE") or def_chunk
    timeout = env.get("SEARCH_TIMEOUT_MS") or def_timeout
    exports = []
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

echo "[worker-autostart] fleet workers=${#fleet_pids[@]} (max ${HACKME_GPU_FLEET_MAX:-20})"
wait "${fleet_pids[@]}"

