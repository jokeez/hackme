#!/usr/bin/env bash
# Continue bounty/CVE hunt after partial runs — wave11 + lowtier + immunefi + discovery.
#
#   setsid bash scripts/ops/run_bounty_continue.sh >>logs/bounty-continue.nohup.log 2>&1 &
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"
export FOUNDRY_OFFLINE="${FOUNDRY_OFFLINE:-true}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/continue-${STAMP}}"
mkdir -p "$OUT"
log() { echo "[continue $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/continue.log"; }

run_step() {
  local id="$1" desc="$2"
  shift 2
  log "=== $id: $desc ==="
  set +e
  "$@" >>"$OUT/${id}.log" 2>&1
  local rc=$?
  set -e
  echo "$rc" >"$OUT/${id}.exit"
  log "$id rc=$rc"
  return 0
}

log "start out=$OUT"

run_step wave11 "OSS CVE wave11 (18 parsers)" \
  env OUT="$ROOT/reports/oss-cve/wave11-${STAMP}" STAMP="$STAMP" \
  bash "$ROOT/scripts/ops/run_oss_cve_wave11.sh"

run_step oss_nightly "OSS CVE nightly rotation (2 targets)" \
  env OUT="$ROOT/reports/oss-cve/nightly-${STAMP}" BUDGET=60000 TIME_LIMIT=600 \
  bash "$ROOT/scripts/ops/run_oss_cve_nightly.sh"

run_step hackenproof_lowtier "Arcadia + Silo Foundry max" \
  env OUT="$OUT/hackenproof-lowtier" FOUNDRY_FUZZ_RUNS=8192 \
  bash "$ROOT/scripts/ops/run_hackenproof_lowtier_push.sh"

run_step immunefi_wasm "Hedera + Wormhole WASM guards" \
  env OUT="$OUT/immunefi-wasm" \
  bash "$ROOT/scripts/ops/run_bounty_hunt_fast.sh"

run_step native_wormhole "Go VAA native probe" \
  env OUT="$OUT/native-wormhole" \
  bash "$ROOT/scripts/ops/immunefi_native_wormhole.sh"

run_step discovery "Foundry discovery sweep" \
  env OUT="$OUT/discovery" FUZZ_RUNS=4096 \
  bash "$ROOT/scripts/ops/run_bounty_discovery_fuzz.sh"

python3 - "$OUT" "$STAMP" <<'PY'
import json, pathlib, sys
out = pathlib.Path(sys.argv[1])
stamp = sys.argv[2]
steps = {}
for f in sorted(out.glob("*.exit")):
    sid = f.stem
    steps[sid] = {"exit_code": int(f.read_text().strip())}
rollup = out / "rollup.json"
candidates = []
for sub in out.rglob("ROLLUP.json"):
    try:
        r = json.loads(sub.read_text())
        if r.get("verdict") in ("CVE_CANDIDATE", "BOUNTY_CANDIDATE", "REVIEW_INTERESTING"):
            candidates.append({"path": str(sub), "verdict": r.get("verdict")})
    except Exception:
        pass
for sub in out.rglob("rollup.json"):
    try:
        r = json.loads(sub.read_text())
        if r.get("candidates"):
            candidates.extend(r.get("candidates"))
        if r.get("verdict") not in (None, "NO_BOUNTY_FINDING", "CLEAN", "BUILD_ISSUES"):
            candidates.append({"path": str(sub), "verdict": r.get("verdict")})
    except Exception:
        pass
body = {"stamp": stamp, "steps": steps, "candidates": candidates}
rollup.write_text(json.dumps(body, indent=2))
print("wrote", rollup)
PY

log "done → $OUT/rollup.json"
