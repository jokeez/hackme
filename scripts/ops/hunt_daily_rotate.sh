#!/usr/bin/env bash
# Local Hunt daily rotator — one OSS catalog target per slot, 24h coverage.
#
# Usage (one slot now):
#   bash scripts/ops/hunt_daily_rotate.sh
#   SLOT_WALL_SEC=1800 bash scripts/ops/hunt_daily_rotate.sh
#   FORCE_TARGET=libucl bash scripts/ops/hunt_daily_rotate.sh
#   DRY_RUN=1 bash scripts/ops/hunt_daily_rotate.sh
#
# Install user timer (hourly):
#   bash scripts/ops/install_hunt_daily_rotate_user.sh
#
# Env:
#   SLOT_WALL_SEC   wall seconds per library (default 3600)
#   HUNT_PKG        hunt_lite|hunt_standard|hunt_heavy (default hunt_standard)
#   FORCE_TARGET    pin one target (skip rotation)
#   DRY_RUN=1       print plan only
#   NICE_LEVEL      default 10
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

SLOT_WALL_SEC="${SLOT_WALL_SEC:-3600}"
HUNT_PKG="${HUNT_PKG:-hunt_standard}"
HUNT_ITER="${HUNT_ITER:-500000}"
NICE_LEVEL="${NICE_LEVEL:-10}"
DAY_UTC="$(date -u +%Y%m%d)"
HOUR_UTC="$(date -u +%H)"
STAMP_UTC="$(date -u +%Y%m%dT%H%M%SZ)"
BASE_OUT="$ROOT/reports/hunt-daily/$DAY_UTC"
LOCK="$ROOT/reports/hunt-daily/.rotate.lock"
mkdir -p "$BASE_OUT" "$ROOT/reports/hunt-daily"

log() { echo "[hunt-daily $(date -u +%H:%M:%S)] $*" | tee -a "$BASE_OUT/rotate.log"; }

exec 9>"$LOCK"
if ! flock -n 9; then
  log "SKIP: another hunt-daily slot holds $LOCK"
  exit 0
fi

if ! command -v clang >/dev/null 2>&1; then
  log "FAIL: clang required"
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  log "FAIL: go required"
  exit 1
fi

pick_target() {
  if [[ -n "${FORCE_TARGET:-}" ]]; then
    echo "$FORCE_TARGET"
    return
  fi
  python3 - "$ROOT" "$DAY_UTC" "$HOUR_UTC" <<'PY'
import json, sys, datetime
root, day, hour = sys.argv[1], sys.argv[2], int(sys.argv[3])
m = json.load(open(f"{root}/upstream/oss_cve_targets.json"))
q = (m.get("rotation") or {}).get("queue") or [t["id"] for t in m.get("targets") or []]
q = [x for x in q if x and x != "csonh"]  # disclosure hold
if not q:
    print("cjson")
    raise SystemExit(0)
d = datetime.datetime.strptime(day, "%Y%m%d")
doy = int(d.strftime("%j"))
idx = (doy * 24 + hour) % len(q)
print(q[idx])
PY
}

TARGET="$(pick_target)"
SLOT_OUT="$BASE_OUT/${HOUR_UTC}00-${TARGET}"
mkdir -p "$SLOT_OUT/crashes"

ENG_LINE="$(grep -E 'const Version[[:space:]]*=' internal/fuzzengine/engine.go | head -1 | sed 's/^[[:space:]]*//')"
log "=== slot day=$DAY_UTC hour=$HOUR_UTC target=$TARGET pkg=$HUNT_PKG wall=${SLOT_WALL_SEC}s ==="
log "engine=${ENG_LINE:-unknown}"
log "out=$SLOT_OUT"

python3 - "$ROOT" "$DAY_UTC" "$BASE_OUT/schedule.json" <<'PY'
import json, sys, datetime
root, day, outp = sys.argv[1], sys.argv[2], sys.argv[3]
m = json.load(open(f"{root}/upstream/oss_cve_targets.json"))
q = [x for x in ((m.get("rotation") or {}).get("queue") or []) if x and x != "csonh"]
d = datetime.datetime.strptime(day, "%Y%m%d")
doy = int(d.strftime("%j"))
slots = [{"hour_utc": f"{h:02d}", "target": q[(doy * 24 + h) % len(q)]} for h in range(24)]
json.dump({"day": day, "slots": slots, "queue_len": len(q)}, open(outp, "w"), indent=2)
print(f"schedule {outp} queue_len={len(q)}")
PY

if [[ "${DRY_RUN:-0}" == "1" ]]; then
  log "DRY_RUN — would build+hunt $TARGET"
  exit 0
fi

log "prebuild OSS pack for $TARGET"
TARGETS="$TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$SLOT_OUT/build.log" 2>&1 || {
  log "FAIL build — see $SLOT_OUT/build.log"
  printf '{"ok":false,"error":"build_failed","target":"%s"}\n' "$TARGET" >"$SLOT_OUT/hunt-local.json"
  python3 "$ROOT/scripts/ops/export_hunt_daily_rollup.py" --day "$DAY_UTC" || true
  exit 1
}

log "START Hunt local"
set +e
nice -n "$NICE_LEVEL" go run ./scripts/tests/tools/hunt_bench_local.go \
  -target "$TARGET" \
  -package "$HUNT_PKG" \
  -iter "$HUNT_ITER" \
  -wall "$SLOT_WALL_SEC" \
  -out "$SLOT_OUT/hunt-local.json" \
  -report "$SLOT_OUT/hunt-report.json" \
  -crashes-dir "$SLOT_OUT/crashes" \
  >"$SLOT_OUT/hunt.log" 2>&1
rc=$?
set -e
log "DONE rc=$rc"

python3 - "$SLOT_OUT" "$TARGET" "$HUNT_PKG" "$SLOT_WALL_SEC" "$STAMP_UTC" "$rc" <<'PY'
import json, sys, os
slot, target, pkg, wall, stamp, rc = sys.argv[1:7]
meta = {
  "stamp": stamp,
  "target": target,
  "package": pkg,
  "wall_sec": int(wall),
  "rc": int(rc),
  "ok": int(rc) == 0 and os.path.isfile(f"{slot}/hunt-local.json"),
}
p = f"{slot}/hunt-local.json"
if os.path.isfile(p):
  hunt = json.load(open(p))
  meta["verdict"] = hunt.get("verdict")
  meta["iterations"] = hunt.get("iterations")
  meta["crashes"] = hunt.get("crashes")
  meta["exec_per_sec"] = hunt.get("exec_per_sec")
  meta["unique_signatures"] = hunt.get("unique_signatures")
  fam = hunt.get("finding_families") or {}
  meta["family_count"] = fam.get("family_count")
json.dump(meta, open(f"{slot}/meta.json", "w"), indent=2)
print(json.dumps(meta))
PY

python3 "$ROOT/scripts/ops/export_hunt_daily_rollup.py" --day "$DAY_UTC"
log "rollup → $BASE_OUT/ROLLUP.md"
exit "$rc"
