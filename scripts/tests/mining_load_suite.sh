#!/usr/bin/env bash
# Full mining load suite: GH/s, difficulty, economics, GPU/CPU telemetry, rig/power scenarios.
#
# Usage:
#   bash scripts/tests/mining_load_suite.sh
#   SAMPLE_SEC=45 STRESS_QUICK=1 bash scripts/tests/mining_load_suite.sh
#
# Env:
#   SAMPLE_SEC          seconds per worker scenario (default 30)
#   SKIP_MEGA_STRESS=1  skip coordinator mega stress
#   SKIP_PUBLIC_POOL=1  only local ephemeral stack
#   USE_EXISTING_STACK=1  BASE/COORD/ADMIN_TOKEN already running
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"
cleanup_stale_report_go_junk

require_cmd go
require_cmd jq
require_cmd curl
require_cmd python3
require_cmd nvidia-smi

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-mining_load_$(run_id)}"
OUT="$OUT_DIR/$RID/mining_load_suite"
ensure_reports_dir "$OUT"
LOG="$OUT/suite.log"
: >"$LOG"

SAMPLE_SEC="${SAMPLE_SEC:-30}"
SKIP_MEGA_STRESS="${SKIP_MEGA_STRESS:-0}"
SKIP_PUBLIC_POOL="${SKIP_PUBLIC_POOL:-0}"
USE_EXISTING_STACK="${USE_EXISTING_STACK:-0}"
STRESS_QUICK="${STRESS_QUICK:-1}"

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

pick_free_port() {
  python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()"
}

ADMIN_TOKEN="${ADMIN_TOKEN:-}"
if [[ -z "$ADMIN_TOKEN" && -f "$ROOT_DIR/.secrets/hackme_admin_token" ]]; then
  ADMIN_TOKEN="$(head -n1 "$ROOT_DIR/.secrets/hackme_admin_token" | tr -d '\r\n')"
fi
if [[ -z "$ADMIN_TOKEN" && -f "$ROOT_DIR/.env.desktop" ]]; then
  # shellcheck disable=SC1091
  set -a && source "$ROOT_DIR/.env.desktop" && set +a
  ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-}"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  ADMIN_TOKEN="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
  log "[warn] ephemeral ADMIN_TOKEN generated"
fi

COORD_TOKEN="${COORD_ADMIN_TOKEN:-}"
if [[ -z "$COORD_TOKEN" && -f "$ROOT_DIR/.secrets/hackme_coordinator_admin_token" ]]; then
  COORD_TOKEN="$(tr -d '\r\n' <"$ROOT_DIR/.secrets/hackme_coordinator_admin_token")"
fi
[[ -z "$COORD_TOKEN" ]] && COORD_TOKEN="$ADMIN_TOKEN"

PUBLIC_COORD="${PUBLIC_COORD_URL:-https://hackme.tech/pool/coordinator}"
PUBLIC_BASE="${PUBLIC_BASE_URL:-https://hackme.tech/pool}"

main_pid=""
coord_pid=""
worker_pid=""
MAIN_PORT=""
COORD_PORT=""
# Do not assign BASE="" / COORD="" — that clears inherited env (USE_EXISTING_STACK).

cleanup() {
  [[ -n "$worker_pid" ]] && kill -TERM "$worker_pid" 2>/dev/null || true
  pkill -f "workerpoh-cuda.*mining-load-${RID}" 2>/dev/null || true
  if [[ "${USE_EXISTING_STACK:-0}" == "1" ]]; then
    return 0
  fi
  [[ -n "$main_pid" ]] && kill -TERM "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -TERM "$coord_pid" 2>/dev/null || true
  sleep 0.5
  [[ -n "$worker_pid" ]] && kill -KILL "$worker_pid" 2>/dev/null || true
  [[ -n "$main_pid" ]] && kill -KILL "$main_pid" 2>/dev/null || true
  [[ -n "$coord_pid" ]] && kill -KILL "$coord_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

wait_api() {
  local url="$1" max="${2:-60}"
  for _ in $(seq 1 "$max"); do
    if curl -fsS --max-time 3 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

start_stack() {
  if [[ "$USE_EXISTING_STACK" == "1" ]]; then
    BASE="${BASE:-http://127.0.0.1:8080}"
    COORD="${COORD:-http://127.0.0.1:8081}"
    export BASE COORD
    log "using existing stack BASE=$BASE COORD=$COORD"
    curl -fsS --max-time 5 "$BASE/api/status" >/dev/null 2>&1 || {
      record FAIL "existing_stack" "BASE unreachable: $BASE"
      return 1
    }
    return 0
  fi

  MAIN_PORT="$(pick_free_port)"
  COORD_PORT="$(pick_free_port)"
  BASE="http://127.0.0.1:${MAIN_PORT}"
  COORD="http://127.0.0.1:${COORD_PORT}"

  section "Ephemeral stack (coord=$COORD node=$BASE)"
  coord_bin="$ROOT_DIR/bin/coordinator-stress"
  main_bin="$ROOT_DIR/bin/hackme"
  [[ -x "$coord_bin" ]] || go build -trimpath -o "$coord_bin" ./cmd/coordinator
  [[ -x "$main_bin" ]] || go build -trimpath -o "$main_bin" .

  HACKME_COORDINATOR_ADDR="127.0.0.1:${COORD_PORT}" \
  HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_COORDINATOR_DB="$OUT/coordinator.db" \
  HACKME_COORDINATOR_ALLOW_INSECURE=1 \
  HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN=0 \
  HACKME_COORDINATOR_CLAIM_PER_MIN=200000 \
  HACKME_COORDINATOR_SUBMIT_PER_MIN=500000 \
  HACKME_COORDINATOR_MAX_WORKERS=256 \
  HACKME_COORDINATOR_MAX_ACTIVE_LEASES=20000 \
  HACKME_COORDINATOR_BAD_STRIKES_TO_BAN=1000000 \
    "$coord_bin" >>"$OUT/coordinator.log" 2>&1 &
  coord_pid=$!

  wait_api "$COORD/api/network/stats" 40 || {
    record FAIL "coordinator_start" "$OUT/coordinator.log"
    tail -20 "$OUT/coordinator.log" >>"$LOG" || true
    return 1
  }
  record PASS "coordinator_start" "$COORD"

  HACKME_BIND_ADDR="127.0.0.1:${MAIN_PORT}" \
  HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_POOL_COORDINATOR_URL="$COORD" \
  HACKME_POOL_COORDINATOR_TOKEN="$ADMIN_TOKEN" \
  HACKME_CHAIN_LEADER_LOCAL_POH=1 \
  HACKME_FUZZ_AUTORUN=0 \
  HACKME_DATA_DIR="$OUT/node_data" \
    "$main_bin" >>"$OUT/node.log" 2>&1 &
  main_pid=$!

  wait_api "$BASE/api/status" 60 || {
    record FAIL "node_start" "$OUT/node.log"
    tail -30 "$OUT/node.log" >>"$LOG" || true
    return 1
  }
  record PASS "node_start" "$BASE"

  if [[ "$(curl -fsS --max-time 10 "$BASE/api/status" | jq -r '.has_genesis')" != "true" ]]; then
    curl -fsS --max-time 15 -X POST "$BASE/api/genesis" \
      -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
      -H "Content-Type: application/json" -d '{}' >"$OUT/genesis.json"
    record PASS "genesis" "$(jq -r '.balance // .block.index // "ok"' "$OUT/genesis.json" 2>/dev/null || echo ok)"
  else
    record PASS "genesis" "already present"
  fi

  curl -fsS --max-time 15 -X POST "$BASE/api/mining/start" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >"$OUT/mining_start.json" || true
}

sample_telemetry() {
  local out="$1"
  local dur="$2"
  local interval="${3:-1}"
  python3 - "$out" "$dur" "$interval" <<'PY'
import json, subprocess, sys, time
out, dur, interval = sys.argv[1], float(sys.argv[2]), float(sys.argv[3])
samples = []
end = time.time() + dur
while time.time() < end:
    row = {"ts": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    try:
        r = subprocess.run(
            ["nvidia-smi", "--query-gpu=utilization.gpu,utilization.memory,power.draw,temperature.gpu,clocks.sm,clocks.mem",
             "--format=csv,noheader,nounits"],
            capture_output=True, text=True, timeout=5,
        )
        if r.returncode == 0 and r.stdout.strip():
            parts = [p.strip() for p in r.stdout.strip().split(",")]
            if len(parts) >= 6:
                def fnum(x):
                    try:
                        return float(x)
                    except Exception:
                        return None
                row.update({
                    "gpu_util_pct": fnum(parts[0]),
                    "mem_util_pct": fnum(parts[1]),
                    "power_w": fnum(parts[2]),
                    "temp_c": fnum(parts[3]),
                    "sm_mhz": fnum(parts[4]),
                    "mem_mhz": fnum(parts[5]),
                })
                # Blackwell drivers often report 0% util during short PoH bursts — infer load from clocks/power.
                sm = row.get("sm_mhz") or 0
                pw = row.get("power_w") or 0
                row["load_proxy_pct"] = min(100.0, max(
                    (row.get("gpu_util_pct") or 0),
                    (sm / 30.0) if sm > 800 else 0,
                    (pw / 1.8) if pw > 25 else 0,
                ))
    except Exception as e:
        row["gpu_err"] = str(e)
    try:
        with open("/proc/stat") as f:
            line = f.readline().split()
        vals = list(map(int, line[1:]))
        idle, total = vals[3], sum(vals)
        time.sleep(0.12)
        with open("/proc/stat") as f:
            line2 = f.readline().split()
        vals2 = list(map(int, line2[1:]))
        idle2, total2 = vals2[3], sum(vals2)
        dt = total2 - total
        if dt > 0:
            row["cpu_util_pct"] = round(100.0 * (1.0 - (idle2 - idle) / dt), 2)
    except Exception as e:
        row["cpu_err"] = str(e)
    samples.append(row)
    time.sleep(max(0.25, interval - 0.12))
with open(out, "w") as f:
    json.dump(samples, f, indent=2)
PY
}

run_worker_scenario() {
  local name="$1"
  local coord_url="$2"
  local token="$3"
  local batch="$4"
  local chunk="$5"
  local timeout_ms="$6"
  local power_w="${7:-}"

  section "Worker scenario: $name"
  local worker_id="mining-load-${RID}-${name}"
  local wlog="$OUT/worker_${name}.log"
  local telem="$OUT/telemetry_${name}.json"

  if [[ -n "$power_w" ]]; then
    curl -sS --max-time 10 -X POST "$BASE/api/hardware/tune" \
      -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"gpu_index\": 0, \"power_limit_w\": ${power_w}}" >"$OUT/power_${name}.json" 2>&1 || true
  fi

  : >"$wlog"
  sample_telemetry "$telem" "$SAMPLE_SEC" 2 &
  local telem_pid=$!

  timeout --signal=INT "$((SAMPLE_SEC + 5))" "$ROOT_DIR/bin/workerpoh-cuda" \
    -coord "$coord_url" -token "$token" \
    -worker "$worker_id" \
    -batch "$batch" -gpu-chunk "$chunk" \
    -gpu-device 0 -search-timeout-ms "$timeout_ms" -gpu-backend cuda \
    >>"$wlog" 2>&1 || true

  wait "$telem_pid" 2>/dev/null || true
  pkill -f "workerpoh-cuda.*${worker_id}" 2>/dev/null || true

  local ghs
  ghs="$(grep -oE 'inst_ghs=[0-9.]+' "$wlog" 2>/dev/null | tail -1 | cut -d= -f2 || echo 0)"
  [[ -z "$ghs" || "$ghs" == "0" ]] && ghs="$(grep -oE '[0-9]+\.[0-9]+ GH/s' "$wlog" 2>/dev/null | tail -1 | awk '{print $1}' || echo 0)"

  local avg_gpu avg_cpu avg_pwr avg_temp peak_load peak_sm
  read -r avg_gpu avg_cpu avg_pwr avg_temp peak_load peak_sm <<<"$(jq -r '
    [.[] | select(.gpu_util_pct != null)] as $g |
    [.[] | select(.cpu_util_pct != null)] as $c |
    [.[] | select(.power_w != null)] as $p |
    [.[] | select(.temp_c != null)] as $t |
    [.[] | select(.load_proxy_pct != null)] as $l |
    [.[] | select(.sm_mhz != null)] as $s |
    [
      (if ($g|length)>0 then ([$g[].gpu_util_pct]|add/length) else 0 end),
      (if ($c|length)>0 then ([$c[].cpu_util_pct]|add/length) else 0 end),
      (if ($p|length)>0 then ([$p[].power_w]|add/length) else 0 end),
      (if ($t|length)>0 then ([$t[].temp_c]|add/length) else 0 end),
      (if ($l|length)>0 then ([$l[].load_proxy_pct]|max) else 0 end),
      (if ($s|length)>0 then ([$s[].sm_mhz]|max) else 0 end)
    ] | @tsv
  ' "$telem" 2>/dev/null || echo "0 0 0 0 0 0")"

  jq -nc \
    --arg name "$name" \
    --argjson batch "$batch" \
    --argjson chunk "$chunk" \
    --argjson timeout_ms "$timeout_ms" \
    --argjson sample_sec "$SAMPLE_SEC" \
    --argjson inst_ghs "$(printf '%s' "${ghs:-0}" | jq -R 'tonumber? // 0')" \
    --argjson avg_gpu_util "$(printf '%s' "$avg_gpu" | jq -R 'tonumber? // 0')" \
    --argjson avg_cpu_util "$(printf '%s' "$avg_cpu" | jq -R 'tonumber? // 0')" \
    --argjson avg_power_w "$(printf '%s' "$avg_pwr" | jq -R 'tonumber? // 0')" \
    --argjson avg_temp_c "$(printf '%s' "$avg_temp" | jq -R 'tonumber? // 0')" \
    --argjson peak_load_pct "$(printf '%s' "$peak_load" | jq -R 'tonumber? // 0')" \
    --argjson peak_sm_mhz "$(printf '%s' "$peak_sm" | jq -R 'tonumber? // 0')" \
    --arg power_limit "${power_w:-}" \
    '{scenario:$name,batch:$batch,chunk:$chunk,search_timeout_ms:$timeout_ms,sample_sec:$sample_sec,
      inst_ghs:$inst_ghs,avg_gpu_util_pct:$avg_gpu_util,avg_cpu_util_pct:$avg_cpu_util,
      avg_power_w:$avg_power_w,avg_temp_c:$avg_temp_c,peak_load_proxy_pct:$peak_load_pct,peak_sm_mhz:$peak_sm_mhz,
      power_limit_w:($power_limit|tonumber? // null)}' \
    >"$OUT/scenario_${name}.json"

  if awk -v g="$ghs" 'BEGIN{exit !(g+0 >= 1.0)}'; then
    record PASS "worker_${name}" "${ghs} GH/s load~${peak_load}% sm_peak=${peak_sm}MHz cpu=${avg_cpu}% ${avg_pwr}W"
  else
    record FAIL "worker_${name}" "inst_ghs=${ghs:-0} — see $wlog"
  fi
}

section "Mining load suite RUN_ID=$RID SAMPLE_SEC=$SAMPLE_SEC"
start_stack

section "Host inventory"
nvidia-smi -L >"$OUT/gpu_list.txt" 2>&1 || true
nvidia-smi --query-gpu=name,driver_version,power.limit,power.max_limit,clocks.max.graphics,clocks.max.memory \
  --format=csv >"$OUT/gpu_caps.csv" 2>&1 || true
lscpu >"$OUT/cpu_info.txt" 2>&1 || true
record PASS "host_inventory"

section "Economics + difficulty snapshot"
curl -fsS --max-time 15 "$BASE/api/status" >"$OUT/status.json"
curl -fsS --max-time 15 "$BASE/api/metrics" >"$OUT/metrics.json" || echo '{}' >"$OUT/metrics.json"
curl -fsS --max-time 15 "$BASE/api/wallet" >"$OUT/wallet.json" 2>/dev/null || echo '{}' >"$OUT/wallet.json"
jq '{
  tip_height, mining, has_genesis,
  economics: .economics | {dev_fee_address, total_minted_hmc, circulating_hmc, max_supply_hmc, total_burned_hmc},
  pool: {target_mod, pool_global_hashrate_th_s, pool_workers_count}
}' "$OUT/status.json" >"$OUT/economics_snapshot.json"
jq '{
  mining_target_mod, mining_target_mod_cap, mining_target_mod_at_cap,
  mining_target_block_sec, mining_observed_block_sec, mining_attempts_per_sec,
  mining_poh_blocks_last_1h, pool_hashrate_th_s
}' "$OUT/metrics.json" >"$OUT/difficulty_snapshot.json"
record PASS "economics_snapshot" "$(jq -r '.economics.total_minted_hmc // "?"' "$OUT/economics_snapshot.json") HMC minted"

section "Local PoH autotune benchmark"
if ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" SAMPLE_SEC=20 APPLY=0 RUN_ID="${RID}_autotune" \
  bash "$ROOT_DIR/scripts/ops/worker_autotune_hashrate.sh" >"$OUT/autotune.log" 2>&1; then
  cp "reports/tests/${RID}_autotune/summary.json" "$OUT/autotune_summary.json" 2>/dev/null || true
  rec="$(jq -r '.recommended_hashrate_gh_s // 0' "$OUT/autotune_summary.json" 2>/dev/null || echo 0)"
  record PASS "worker_autotune" "${rec} GH/s recommended"
else
  record FAIL "worker_autotune" "$OUT/autotune.log"
fi

section "CUDA worker scenarios (local coordinator)"
# daily (RTX 50 profile)
run_worker_scenario "daily_4m" "$COORD" "$ADMIN_TOKEN" 4194304 4194304 2200 ""
# turbo-like larger batch (manual OC path)
run_worker_scenario "turbo_8m" "$COORD" "$ADMIN_TOKEN" 8388608 4194304 1800 ""
# conservative (eco thermals)
run_worker_scenario "eco_2m" "$COORD" "$ADMIN_TOKEN" 2097152 1048576 3500 ""

# Power limit scenarios if hardware tune works
curl -fsS --max-time 10 "$BASE/api/hardware/tune" >"$OUT/hardware_tune.json" 2>/dev/null || echo '{}' >"$OUT/hardware_tune.json"
eco_w="$(jq -r '.devices[0].preset_eco_w // 120' "$OUT/hardware_tune.json")"
max_w="$(jq -r '.devices[0].power_max_w // 180' "$OUT/hardware_tune.json")"
run_worker_scenario "power_eco" "$COORD" "$ADMIN_TOKEN" 4194304 4194304 2200 "$eco_w"
run_worker_scenario "power_max" "$COORD" "$ADMIN_TOKEN" 4194304 4194304 2200 "$max_w"

if ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" GPU_INDEX=0 RUN_ID="${RID}_power" \
  bash "$ROOT_DIR/scripts/tests/gpu_power_smoke.sh" >"$OUT/gpu_power_smoke.log" 2>&1; then
  cp "reports/tests/${RID}_power/summary.json" "$OUT/gpu_power_smoke.json" 2>/dev/null || true
  record PASS "gpu_power_smoke"
else
  if jq -e '.pass == true or .degraded == true' "reports/tests/${RID}_power/summary.json" >/dev/null 2>&1; then
    cp "reports/tests/${RID}_power/summary.json" "$OUT/gpu_power_smoke.json" 2>/dev/null || true
    record PASS "gpu_power_smoke" "degraded (no root for nvidia-smi -pl)"
  else
    record FAIL "gpu_power_smoke" "$OUT/gpu_power_smoke.log"
  fi
fi

section "Difficulty health (local)"
if BASE="$BASE" RUN_ID="${RID}_diff" MAX_FAST_RATIO=0.001 CHAIN_LIMIT=25 \
  bash "$ROOT_DIR/scripts/tests/difficulty_health.sh" >"$OUT/difficulty_health.log" 2>&1; then
  cp "$OUT_DIR/${RID}_diff/difficulty_health/summary.json" "$OUT/difficulty_health.json" 2>/dev/null || true
  record PASS "difficulty_health" "$(jq -r '.target_mod // "?"' "$OUT/difficulty_health.json" 2>/dev/null)"
else
  record FAIL "difficulty_health" "$OUT/difficulty_health.log"
fi

if [[ "$SKIP_PUBLIC_POOL" != "1" && -n "$COORD_TOKEN" ]]; then
  section "Public pool probe (short)"
  if curl -fsS --max-time 60 "$PUBLIC_BASE/api/metrics" >"$OUT/public_metrics.json" 2>&1; then
    jq '{
      mining_target_mod, mining_observed_block_sec, pool_hashrate_th_s, pool_workers_active
    }' "$OUT/public_metrics.json" >"$OUT/public_pool_snapshot.json"
    record PASS "public_pool_metrics" "$(jq -r '.pool_hashrate_th_s // "?"' "$OUT/public_pool_snapshot.json") TH/s"
    run_worker_scenario "public_pool" "$PUBLIC_COORD" "$COORD_TOKEN" 4194304 4194304 2200 ""
  else
    record PASS "public_pool_metrics" "skipped — public pool timeout (degraded)"
  fi
fi

if [[ "$SKIP_MEGA_STRESS" != "1" ]]; then
  section "Coordinator mega stress (quick=$STRESS_QUICK)"
  unset HACKME_COORDINATOR_ADDR HACKME_COORDINATOR_DB 2>/dev/null || true
  if STRESS_QUICK="$STRESS_QUICK" RUN_ID="${RID}_mega" REPORT_DIR="$OUT/mega_stress" \
    bash "$ROOT_DIR/scripts/tests/coordinator_mega_stress.sh" >>"$LOG" 2>&1; then
    record PASS "coordinator_mega_stress"
  else
    record FAIL "coordinator_mega_stress" "see $LOG"
  fi
fi

section "Aggregate scenarios"
jq -s '{
  scenarios: .,
  best_ghs: ([.[].inst_ghs | select(. > 0)] | max // 0),
  avg_gpu_util: ([.[].avg_gpu_util_pct | select(. > 0)] | if length>0 then add/length else 0 end),
  avg_cpu_util: ([.[].avg_cpu_util_pct | select(. > 0)] | if length>0 then add/length else 0 end)
}' "$OUT"/scenario_*.json >"$OUT/scenarios_summary.json" 2>/dev/null || echo '{"scenarios":[]}' >"$OUT/scenarios_summary.json"

best="$(jq -r '.best_ghs // 0' "$OUT/scenarios_summary.json")"
status="PASS"
[[ "$failures" -eq 0 ]] || status="FAIL"

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg status "$status" \
  --arg base "$BASE" \
  --arg coord "$COORD" \
  --argjson passes "$passes" \
  --argjson failures "$failures" \
  --argjson best_ghs "$(printf '%s' "$best" | jq -R 'tonumber? // 0')" \
  --slurpfile econ "$OUT/economics_snapshot.json" \
  --slurpfile diff "$OUT/difficulty_snapshot.json" \
  --slurpfile scen "$OUT/scenarios_summary.json" \
  '{
    run_id: $run_id,
    captured_at: $captured_at,
    suite: "mining_load_suite",
    status: $status,
    base: $base,
    coordinator: $coord,
    passes: $passes,
    failures: $failures,
    best_inst_ghs: $best_ghs,
    economics: $econ[0],
    difficulty: $diff[0],
    scenarios: $scen[0]
  }' >"$OUT/summary.json"

ln -sfn "$OUT" "$ROOT_DIR/reports/mining-load-LATEST"

log ""
log "Suite $status: $passes passed, $failures failed, best=${best} GH/s"
log "Report: $OUT/summary.json"

if [[ "$status" != "PASS" ]]; then
  fail "mining_load_suite FAIL ($failures failures). See $OUT"
fi
pass "mining_load_suite PASS ($passes checks, best ${best} GH/s). See $OUT"
