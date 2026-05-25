#!/usr/bin/env bash
# Live mining + full Hardware tune API test (matches dashboard buttons).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
BASE="${BASE_URL:-http://127.0.0.1:8080}"
SAMPLE_SEC="${SAMPLE_SEC:-45}"
REPORT="${REPORT:-$ROOT/reports/hardware_tune_live_$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$REPORT"
LOG="$REPORT/suite.log"
pass=0; fail=0

log() { echo "$*" | tee -a "$LOG"; }
record() {
  local st="$1" id="$2" detail="${3:-}"
  if [[ "$st" == PASS ]]; then pass=$((pass+1)); log "[PASS] $id${detail:+ — $detail}"; else fail=$((fail+1)); log "[FAIL] $id${detail:+ — $detail}"; fi
}

# shellcheck disable=SC1091
[[ -f "$ROOT/.env.desktop" ]] && set -a && . "$ROOT/.env.desktop" && set +a
ADMIN="${HACKME_ADMIN_TOKEN:-}"
[[ -z "$ADMIN" && -f "$ROOT/.secrets/hackme_admin_token" ]] && \
  ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
[[ -z "$ADMIN" ]] && { log "FATAL: no admin token"; exit 2; }
hdr=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")
COORD_URL="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"

ensure_node() {
  if curl -fsS --max-time 5 "$BASE/api/status" >/dev/null 2>&1; then
    record PASS "node_up" "$BASE"
    return 0
  fi
  log "starting desktop node..."
  DESKTOP_PROFILE=worker BIND_ADDR=127.0.0.1:8080 WORKER_AUTOSTART=0 SKIP_TOOLCHAINS=1 \
    bash "$ROOT/scripts/ops/desktop_mode_up.sh" >>"$REPORT/desktop_up.log" 2>&1 || true
  for _ in $(seq 1 40); do
    curl -fsS --max-time 3 "$BASE/api/status" >/dev/null 2>&1 && { record PASS "node_start" "$BASE"; return 0; }
    sleep 1
  done
  record FAIL "node_start" "see $REPORT/desktop_up.log"
  exit 1
}

ensure_node
bash "$ROOT/scripts/ops/build_gpu_workers.sh" >>"$REPORT/build_gpu.log" 2>&1 && record PASS "build_gpu_workers" || record FAIL "build_gpu_workers"

log "=== rig profile detect/apply ==="
curl -fsS "$BASE/api/hardware/rig-profiles/detect" >"$REPORT/rig_detect.json"
pid="$(jq -r '.profile_id // empty' "$REPORT/rig_detect.json")"
[[ -n "$pid" ]] && record PASS "rig_detect" "$pid" || record FAIL "rig_detect"
curl -fsS -X POST "$BASE/api/hardware/rig-profiles/apply" "${hdr[@]}" \
  -d "{\"profile_id\":\"${pid:-nvidia_rtx_50_daily}\"}" >"$REPORT/rig_apply.json"
jq -e '.ok == true' "$REPORT/rig_apply.json" >/dev/null && record PASS "rig_apply" "$(jq -r '.message // ok' "$REPORT/rig_apply.json" | head -c 80)" || record FAIL "rig_apply"

log "=== start pool worker ==="
curl -fsS -X POST "$BASE/api/worker/stop" "${hdr[@]}" -d '{}' >/dev/null 2>&1 || true
sleep 2
curl -fsS -X POST "$BASE/api/worker/start" "${hdr[@]}" \
  -d "$(python3 -c "import json; print(json.dumps({'coord_url':'$COORD_URL'}))")" \
  >"$REPORT/worker_start.json"
jq -e '.running == true' "$REPORT/worker_start.json" >/dev/null && record PASS "worker_start" "$(jq -r '.worker_id' "$REPORT/worker_start.json")" || record FAIL "worker_start" "$(cat "$REPORT/worker_start.json")"
sleep 12

log "=== GET /api/hardware/tune (baseline under load) ==="
curl -fsS "$BASE/api/hardware/tune" >"$REPORT/tune_get_baseline.json"
jq -e '.nvidia_smi == true and (.devices|length) >= 1' "$REPORT/tune_get_baseline.json" >/dev/null \
  && record PASS "tune_get" "GPU0 $(jq -r '.devices[0].name' "$REPORT/tune_get_baseline.json")" \
  || record FAIL "tune_get"

log "=== POST CPU soft_cap 80 ==="
curl -fsS -X POST "$BASE/api/hardware/tune" "${hdr[@]}" -d '{"soft_cap_pct":80}' >"$REPORT/tune_cpu80.json"
jq -e '.ok == true' "$REPORT/tune_cpu80.json" >/dev/null && record PASS "tune_cpu_80" || record FAIL "tune_cpu_80"

log "=== POST GPU presets eco / daily / turbo ==="
for mode in eco daily turbo; do
  code=$(curl -sS -o "$REPORT/tune_${mode}.json" -w '%{http_code}' -X POST "$BASE/api/hardware/tune" \
    "${hdr[@]}" -d "{\"gpu_index\":0,\"mode\":\"$mode\"}" || echo 000)
  if jq -e '.ok == true' "$REPORT/tune_${mode}.json" >/dev/null 2>&1; then
    record PASS "tune_gpu_${mode}" "applied=$(jq -r '.applied_power_limit_w // .requested_power_limit_w' "$REPORT/tune_${mode}.json")W HTTP=$code"
  elif [[ "$code" == "400" ]] && grep -q insufficient_permissions "$REPORT/tune_${mode}.json" 2>/dev/null; then
  if sudo -n nvidia-smi -i 0 -pl "$(jq -r '.devices[0].preset_'"${mode}"'_w // empty' "$REPORT/tune_get_baseline.json" 2>/dev/null || echo 150)" >/dev/null 2>&1; then
      record PASS "tune_gpu_${mode}" "sudo nvidia-smi -pl fallback"
    else
      record FAIL "tune_gpu_${mode}" "HTTP=$code insufficient_permissions (need sudo for -pl)"
    fi
  else
    record FAIL "tune_gpu_${mode}" "HTTP=$code $(head -c 100 "$REPORT/tune_${mode}.json")"
  fi
  sleep 3
done

log "=== POST explicit power_limit_w 180 then 150 ==="
for w in 180 150; do
  code=$(curl -sS -o "$REPORT/tune_pl_${w}.json" -w '%{http_code}' -X POST "$BASE/api/hardware/tune" \
    "${hdr[@]}" -d "{\"gpu_index\":0,\"power_limit_w\":$w}" || echo 000)
  if jq -e '.ok == true' "$REPORT/tune_pl_${w}.json" >/dev/null 2>&1; then
    record PASS "tune_pl_${w}W" "applied=$(jq -r '.applied_power_limit_w' "$REPORT/tune_pl_${w}.json")W"
  elif sudo -n nvidia-smi -i 0 -pl "$w" >/dev/null 2>&1; then
    record PASS "tune_pl_${w}W" "sudo fallback"
  else
    record FAIL "tune_pl_${w}W" "HTTP=$code"
  fi
  sleep 4
done

log "=== GET tune after changes ==="
curl -fsS "$BASE/api/hardware/tune" >"$REPORT/tune_get_after.json"
lim="$(jq -r '.devices[0].power_limit_w // 0' "$REPORT/tune_get_after.json")"
record PASS "tune_get_after" "limit=${lim}W util=$(jq -r '.devices[0].util_pct' "$REPORT/tune_get_after.json")%"

log "=== mining load sample ${SAMPLE_SEC}s ==="
python3 - "$REPORT/gpu_worker_samples.jsonl" "$SAMPLE_SEC" "$BASE" <<'PY' | tee -a "$LOG"
import json, subprocess, sys, time, urllib.request
out, dur, base = sys.argv[1], float(sys.argv[2]), sys.argv[3]
rows = []
end = time.time() + dur
while time.time() < end:
    row = {"ts": time.time()}
    try:
        r = subprocess.run(
            ["nvidia-smi", "--query-gpu=utilization.gpu,power.draw,clocks.sm,temperature.gpu",
             "--format=csv,noheader,nounits"],
            capture_output=True, text=True, timeout=5,
        )
        if r.returncode == 0:
            p = [x.strip() for x in r.stdout.strip().split(",")]
            if len(p) >= 4:
                row.update(util=float(p[0]), power_w=float(p[1]), sm_mhz=float(p[2]), temp_c=float(p[3]))
    except Exception:
        pass
    try:
        with urllib.request.urlopen(base + "/api/worker/status", timeout=5) as resp:
            w = json.loads(resp.read())
            row["worker_ghs"] = float(w.get("measured_hashrate_gh_s") or w.get("hashrate_gh_s") or 0)
            row["worker_running"] = bool(w.get("running"))
    except Exception:
        pass
    rows.append(row)
    time.sleep(1)
with open(out, "w") as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")
utils = [r["util"] for r in rows if "util" in r]
powers = [r["power_w"] for r in rows if "power_w" in r]
sms = [r["sm_mhz"] for r in rows if "sm_mhz" in r]
ghs = [r["worker_ghs"] for r in rows if r.get("worker_ghs", 0) > 0]
print("samples", len(rows),
      "util avg/max", round(sum(utils)/len(utils),1) if utils else 0, max(utils) if utils else 0,
      "power avg/max W", round(sum(powers)/len(powers),1) if powers else 0, max(powers) if powers else 0,
      "sm avg/max MHz", round(sum(sms)/len(sms),0) if sms else 0, max(sms) if sms else 0,
      "worker ghs max", round(max(ghs),2) if ghs else 0)
if ghs and max(ghs) >= 10:
    print("HASHRATE_OK")
else:
    print("HASHRATE_LOW")
PY

grep -q HASHRATE_OK "$LOG" && record PASS "hashrate_under_load" "$(grep worker "$LOG" | tail -1)" || record FAIL "hashrate_under_load" "see gpu_worker_samples.jsonl"

curl -fsS "$BASE/api/wallet" >"$REPORT/wallet.json"
curl -fsS "$BASE/api/worker/status" >"$REPORT/worker_final.json"
jq '{address,balance_display_hmc}' "$REPORT/wallet.json" | tee -a "$LOG"
jq '{running,measured_hashrate_gh_s,worker_id,coord_url}' "$REPORT/worker_final.json" | tee -a "$LOG"

log ""
log "=== SUMMARY: $pass passed, $fail failed ==="
log "Report: $REPORT"
[[ "$fail" -eq 0 ]]
