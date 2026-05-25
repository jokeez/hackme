#!/usr/bin/env bash
# Start desktop node + full mining + verify /api/hardware/tune (OC buttons backend).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
BASE="${BASE_URL:-http://127.0.0.1:8080}"
REPORT="${REPORT:-$ROOT/reports/desktop_tune_check_$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$REPORT"

log() { echo "$*" | tee -a "$REPORT/run.log"; }

# shellcheck disable=SC1091
[[ -f "$ROOT/.env.desktop" ]] && set -a && . "$ROOT/.env.desktop" && set +a
ADMIN="${HACKME_ADMIN_TOKEN:-}"
[[ -z "$ADMIN" && -f "$ROOT/.secrets/hackme_admin_token" ]] && \
  ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
[[ -z "$ADMIN" ]] && { log "FATAL: no admin token"; exit 2; }

log "=== stop old desktop ==="
bash "$ROOT/scripts/ops/desktop_mode_stop.sh" >>"$REPORT/run.log" 2>&1 || true

log "=== build GPU workers ==="
bash "$ROOT/scripts/ops/build_gpu_workers.sh" >>"$REPORT/build_gpu.log" 2>&1

# Avoid logs/desktop/data when WAL grew huge (node hangs on sqlite open).
DESKTOP_DATA="$ROOT/logs/desktop/data"
if [[ -f "$DESKTOP_DATA/hackme.db-wal" ]]; then
  wal_bytes=$(stat -c%s "$DESKTOP_DATA/hackme.db-wal" 2>/dev/null || echo 0)
  if [[ "$wal_bytes" -gt 536870912 ]]; then
    log "WARN: logs/desktop/data WAL is ${wal_bytes} bytes — using $ROOT/data instead"
    export HACKME_DATA_DIR="$ROOT/data"
  fi
fi

log "=== start desktop (command = local PoH + CUDA node) ==="
DESKTOP_PROFILE=command BIND_ADDR=127.0.0.1:8080 BASE_URL="$BASE" \
  WORKER_AUTOSTART=0 SKIP_TOOLCHAINS=1 \
  bash "$ROOT/scripts/ops/desktop_mode_up.sh" >>"$REPORT/desktop_up.log" 2>&1

hdr=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

log "=== genesis + mining + pool worker ==="
curl -fsS -X POST "$BASE/api/genesis" "${hdr[@]}" -d '{}' >"$REPORT/genesis.json" 2>/dev/null || true
curl -fsS -X POST "$BASE/api/mining/start" "${hdr[@]}" -d '{}' >"$REPORT/mining_start.json"
COORD_URL="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
curl -fsS -X POST "$BASE/api/worker/start" "${hdr[@]}" \
  -d "$(python3 -c "import json; print(json.dumps({'coord_url':'$COORD_URL'}))")" \
  >"$REPORT/worker_start.json"

sleep 3
curl -fsS "$BASE/api/status" >"$REPORT/status.json"
curl -fsS "$BASE/api/metrics" >"$REPORT/metrics.json"
curl -fsS "$BASE/api/worker/status" >"$REPORT/worker_status.json"

log "=== GET /api/hardware/tune ==="
curl -fsS "$BASE/api/hardware/tune" >"$REPORT/hardware_tune_get.json"
python3 - "$REPORT/hardware_tune_get.json" <<'PY' | tee -a "$REPORT/run.log"
import json, sys
j = json.load(open(sys.argv[1]))
print("nvidia_smi:", j.get("nvidia_smi"), "devices:", len(j.get("devices") or []))
for d in j.get("devices") or []:
    print("  GPU%d %s util=%.0f%% power=%.0fW limit=%.0fW eco=%.0f daily=%.0f turbo=%.0f" % (
        d.get("index",0), (d.get("name") or "")[:40],
        d.get("util_pct") or 0, d.get("power_draw_w") or 0, d.get("power_limit_w") or 0,
        d.get("preset_eco_w") or 0, d.get("preset_daily_w") or 0, d.get("preset_turbo_w") or 0,
    ))
PY

log "=== POST hardware tune: CPU soft cap 85% ==="
curl -fsS -X POST "$BASE/api/hardware/tune" "${hdr[@]}" -d '{"soft_cap_pct":85}' >"$REPORT/tune_cpu.json"

log "=== POST GPU presets (eco, daily, turbo) ==="
for mode in eco daily turbo; do
  code=$(curl -sS -o "$REPORT/tune_gpu_${mode}.json" -w '%{http_code}' -X POST "$BASE/api/hardware/tune" \
    "${hdr[@]}" -d "{\"gpu_index\":0,\"mode\":\"$mode\"}" || echo 000)
  log "  mode=$mode HTTP=$code $(head -c 120 "$REPORT/tune_gpu_${mode}.json")"
  sleep 2
done

log "=== GPU load sample 20s (mining should load GPU) ==="
python3 - "$REPORT/gpu_samples.jsonl" <<'PY' | tee -a "$REPORT/run.log"
import json, subprocess, sys, time
out = sys.argv[1]
end = time.time() + 20
with open(out, "w") as f:
    while time.time() < end:
        r = subprocess.run(
            ["nvidia-smi", "--query-gpu=utilization.gpu,power.draw,clocks.sm,temperature.gpu",
             "--format=csv,noheader,nounits"],
            capture_output=True, text=True, timeout=5,
        )
        row = {"ts": time.time()}
        if r.returncode == 0:
            p = [x.strip() for x in r.stdout.strip().split(",")]
            if len(p) >= 4:
                row.update(util=float(p[0]), power_w=float(p[1]), sm_mhz=float(p[2]), temp_c=float(p[3]))
        f.write(json.dumps(row) + "\n")
        time.sleep(1)
# summary
rows = [json.loads(l) for l in open(out) if l.strip()]
utils = [r["util"] for r in rows if "util" in r]
powers = [r["power_w"] for r in rows if "power_w" in r]
print("samples:", len(rows), "util avg/max:", (sum(utils)/len(utils) if utils else 0), (max(utils) if utils else 0),
      "power avg/max W:", (sum(powers)/len(powers) if powers else 0), (max(powers) if powers else 0))
PY

log "=== final metrics ==="
curl -fsS "$BASE/api/metrics" | python3 -c 'import sys,json; m=json.load(sys.stdin); print("attempts/s", m.get("mining_attempts_per_sec"), "workers", m.get("mining_workers"), "backend", m.get("mining_poh_backend"))' | tee -a "$REPORT/run.log"
curl -fsS "$BASE/api/worker/status" | python3 -c 'import sys,json; w=json.load(sys.stdin); print("pool worker running", w.get("running"), "ghs", w.get("hashrate_gh_s"))' | tee -a "$REPORT/run.log"

log ""
log "Dashboard: $BASE → tab Hardware tune (g then h)"
log "Report: $REPORT"
