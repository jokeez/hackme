#!/usr/bin/env bash
# ~1h Hunt local marathon + engine A/B + 40-miner fleet projection.
# Usage:
#   bash scripts/tests/hunt_hour_marathon.sh
#   TARGET=libucl WALL_SEC=3600 bash scripts/tests/hunt_hour_marathon.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET="${TARGET:-libucl}"
PKG="${PKG:-hunt_standard}"
WALL_SEC="${WALL_SEC:-3600}"
# High budget so wall clock is the limiter (libucl ~30-40 exec/s → ~100k+/h)
HUNT_ITER="${HUNT_ITER:-500000}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/hunt-marathon/$STAMP"
mkdir -p "$OUT"

log() { echo "[marathon $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

log "=== Hunt marathon stamp=$STAMP target=$TARGET wall=${WALL_SEC}s iter_budget=$HUNT_ITER pkg=$PKG ==="
log "engine=$(grep -E '^\s*Version\s*=' internal/fuzzengine/engine.go | head -1)"

# Prebuild ASAN driver if needed
if ! command -v clang >/dev/null 2>&1; then
  log "FAIL: clang required"
  exit 1
fi
log "prebuild OSS pack for $TARGET"
TARGETS="$TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build.log" 2>&1 || {
  log "build pack failed — see $OUT/build.log"
  exit 1
}

# Engine mutation A/B (fast, parallel-safe)
log "engine A/B heavy (mutation diversity)"
go test ./internal/fuzzengine/ -run 'TestEngineABHeavyLocal|TestEngineABComparison' -v -count=1 -timeout 300s \
  2>&1 | tee "$OUT/engine-ab.log" | tail -30

# Fleet projection helper (uses measured eps after hunt; placeholder until then)
cat >"$OUT/fleet_project.py" <<'PY'
#!/usr/bin/env python3
import json, math, sys
eps = float(sys.argv[1])          # measured local exec/s
iters = int(sys.argv[2])          # iterations completed in wall
wall = float(sys.argv[3])         # wall seconds
crashes = int(sys.argv[4])
verdict = sys.argv[5]
unique_families = int(sys.argv[6]) if len(sys.argv) > 6 else 0
miners = 40
# Hybrid dig efficiency: not all cores dig 100% (PoH share) — use 0.55–0.75 band
eff_lo, eff_hi = 0.55, 0.75
fleet_eps_lo = eps * miners * eff_lo
fleet_eps_hi = eps * miners * eff_hi
# Wall to reach same iteration budget as this run
budget = max(iters, 1)
wall_fleet_lo = budget / fleet_eps_hi if fleet_eps_hi > 0 else None
wall_fleet_hi = budget / fleet_eps_lo if fleet_eps_lo > 0 else None
# Hunt Standard package: 4000 shards × 128 iter ≈ 512k exec if 1-exec-per-iter model
std_exec = 4000 * 128
out = {
  "local": {
    "exec_per_sec": eps,
    "iterations": iters,
    "wall_sec": wall,
    "crashes": crashes,
    "verdict": verdict,
    "unique_stack_or_family_hint": unique_families,
  },
  "fleet_40_hybrid_estimate": {
    "note": "Linear scale × efficiency (hybrid digs while mining). Not perfect Amdahl — claim/replay contention reduces real speedup.",
    "efficiency_band": [eff_lo, eff_hi],
    "est_exec_per_sec": [round(fleet_eps_lo, 1), round(fleet_eps_hi, 1)],
    "est_wall_sec_same_iters": [
      None if wall_fleet_lo is None else round(wall_fleet_lo, 1),
      None if wall_fleet_hi is None else round(wall_fleet_hi, 1),
    ],
    "speedup_vs_1_miner": [round(miners * eff_lo, 1), round(miners * eff_hi, 1)],
    "hunt_standard_512k_exec_wall_sec_est": [
      round(std_exec / fleet_eps_hi, 1) if fleet_eps_hi else None,
      round(std_exec / fleet_eps_lo, 1) if fleet_eps_lo else None,
    ],
  },
  "depth_note": "More miners ⇒ more parallel shards ⇒ more diverse stage/salt space covered per wall-hour ⇒ faster campaign close + broader input diversity. Per-core ASAN still slower than libFuzzer.",
}
print(json.dumps(out, indent=2))
open(sys.argv[7], "w").write(json.dumps(out, indent=2))
PY

log "START Hunt local wall=${WALL_SEC}s (this is the long soak)"
JSON="$OUT/hunt-local-${TARGET}.json"
REPORT="$OUT/hunt-report-${TARGET}.json"
CRASHES="$OUT/crashes-${TARGET}"
mkdir -p "$CRASHES"

set +e
go run ./scripts/tests/tools/hunt_bench_local.go \
  -target "$TARGET" \
  -package "$PKG" \
  -iter "$HUNT_ITER" \
  -wall "$WALL_SEC" \
  -out "$JSON" \
  -report "$REPORT" \
  -crashes-dir "$CRASHES" \
  2>&1 | tee "$OUT/hunt-local-${TARGET}.log"
hunt_rc=$?
set -e

if [[ ! -f "$JSON" ]]; then
  log "FAIL: no hunt json (rc=$hunt_rc)"
  exit 1
fi

python3 - "$JSON" "$OUT/fleet-40.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
eps = float(d.get("exec_per_sec") or 0)
iters = int(d.get("iterations") or 0)
wall = float(d.get("elapsed_sec") or d.get("wall_sec") or 0)
crashes = int(d.get("crashes") or 0)
verdict = str(d.get("verdict") or "")
by = d.get("stack_frames") or d.get("by_stack") or {}
if isinstance(by, dict):
  families = len(by)
elif isinstance(d.get("unique_stack_frames"), int):
  families = int(d["unique_stack_frames"])
print(f"verdict={verdict} iter={iters} crashes={crashes} elapsed={wall:.1f}s eps={eps:.2f} unique_stacks={families}")
import subprocess, os
root = os.path.dirname(sys.argv[2])
subprocess.check_call([
  sys.executable, os.path.join(root, "fleet_project.py"),
  str(eps), str(iters), str(wall), str(crashes), verdict, str(families), sys.argv[2],
])
PY

log "SUMMARY written under $OUT"
log "fleet projection: $OUT/fleet-40.json"
ls -la "$OUT" | tee -a "$OUT/run.log"
exit 0
