#!/usr/bin/env bash
# Wait for wave14 (+ asan-chase) to finish, then discovery fuzz + wave15/16 easy OSS CVE hunts.
#
#   setsid bash scripts/ops/run_bounty_hunt_after_wave14.sh >>logs/bounty-wave15-pipeline.nohup.log 2>&1 &
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/bounty/wave15-pipeline-${STAMP}}"
mkdir -p "$OUT" "$ROOT/logs"
log() { echo "[after-wave14 $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/orchestrator.log"; }

wait_pid() {
  local pid="$1" label="$2"
  [[ -n "$pid" ]] || return 0
  if kill -0 "$pid" 2>/dev/null; then
    log "wait $label pid=$pid"
    while kill -0 "$pid" 2>/dev/null; do sleep 45; done
    log "$label done"
  fi
}

wait_pattern() {
  local pat="$1" label="$2"
  local pid
  pid="$(pgrep -f "$pat" | head -1 || true)"
  wait_pid "$pid" "$label"
}

log "=== wave15 pipeline start ==="

# 1) Wait for wave14 asan-chase (and any stray wave14 hunt)
wait_pattern 'wave14-asan-chase.*oss_cve_hunt' 'wave14-asan-chase'
wait_pattern 'run_bounty_hunt_after_wave13' 'after-wave13 orchestrator'

# Confirm main wave14 rollup exists
W14="$(ls -td "$ROOT/reports/oss-cve/wave14-"*/ROLLUP.json 2>/dev/null | head -1 || true)"
if [[ -z "$W14" ]]; then
  log "WARN: no wave14 ROLLUP — running wave14 first"
  bash "$ROOT/scripts/ops/run_oss_cve_wave14.sh" >>"$OUT/wave14-missing.log" 2>&1 || true
else
  log "wave14 rollup ok: $W14"
fi

# 2) Refresh public bounty repo list + discovery fuzz (Solidity easy targets)
log "fetch hackenproof-public repos"
bash "$ROOT/scripts/ops/fetch_bounty_programs.sh" >>"$OUT/discovery.log" 2>&1 || log "fetch_bounty_programs skipped"

log "discovery fuzz (foundry repos)"
FUZZ_RUNS="${DISCOVERY_FUZZ_RUNS:-2048}" \
  OUT="$OUT/discovery-fuzz" \
  bash "$ROOT/scripts/ops/run_bounty_discovery_fuzz.sh" >>"$OUT/discovery.log" 2>&1 || true

# 3) Wave15 — easy OSS CVE targets (never fuzzed / under-tested parsers)
log "launch wave15 easy OSS CVE hunt"
set +e
WAVE=15 TOP="${WAVE15_TOP:-14}" bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$OUT/wave15.log" 2>&1
W15_RC=$?
set -e
log "wave15 rc=$W15_RC"

# 4) ASAN chase on wave15 INFORMATIONAL with high crash count
CAND="$(python3 -c "
import json, pathlib
root = pathlib.Path('$ROOT/reports/oss-cve')
waves = sorted(root.glob('wave15-*/ROLLUP.json'))
if not waves: raise SystemExit(0)
r = json.loads(waves[-1].read_text())
ids = []
for t in r.get('targets', []):
    if t.get('verdict')=='INFORMATIONAL' and len(t.get('crashes') or [])>30:
        ids.append(t.get('target_id'))
print(','.join(x for x in ids if x))
" 2>/dev/null || true)"

if [[ -n "$CAND" ]]; then
  log "wave15 asan-chase: $CAND"
  WAVE=15 STAMP="${STAMP}-asan-chase" \
    OUT="$ROOT/reports/oss-cve/wave15-asan-chase-${STAMP}" \
    TARGETS="$CAND" BUDGET=350000 TIME_LIMIT=10800 \
    bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$OUT/wave15-asan-chase.log" 2>&1 || true
fi

# 5) Wave16 — sweep remaining never-fuzzed easy parsers (wider net, lower budget each)
log "launch wave16 never-fuzzed sweep"
set +e
WAVE=16 TOP="${WAVE16_TOP:-18}" bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$OUT/wave16.log" 2>&1
W16_RC=$?
set -e
log "wave16 rc=$W16_RC"

# 6) Rollup summary
python3 - "$OUT" "$ROOT" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
root = pathlib.Path(sys.argv[2])
summary = {"stamp": out.name, "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "waves": {}}
for wave in (14, 15, 16):
    pat = f"wave{wave}-"
    rolls = sorted((root / "reports/oss-cve").glob(f"{pat}*/ROLLUP.json"), key=lambda p: p.stat().st_mtime)
    if not rolls:
        continue
    r = json.loads(rolls[-1].read_text())
    summary["waves"][str(wave)] = {
        "path": str(rolls[-1]),
        "verdict": r.get("verdict"),
        "summary": (r.get("summary") or "")[:300],
        "informational": r.get("informational_targets") or [],
        "clean": r.get("clean_targets") or [],
    }
disc = out / "discovery-fuzz" / "rollup.json"
if disc.is_file():
    d = json.loads(disc.read_text())
    summary["discovery"] = {"verdict": d.get("verdict"), "candidates": len(d.get("candidates") or [])}
(out / "pipeline_rollup.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2))
PY

log "=== pipeline done — $OUT/pipeline_rollup.json ==="
exit 0
