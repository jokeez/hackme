#!/usr/bin/env bash
# Day-long OSS CVE marathon: wave15 through wave25 (11 waves), distinct target queues each run.
#
#   setsid bash scripts/ops/run_oss_cve_day_marathon.sh >>logs/oss-cve-day-marathon.nohup.log 2>&1 &
#   tail -f logs/oss-cve-day-marathon.nohup.log
#
# Env:
#   WAVE_FIRST=15 WAVE_LAST=25   — default 15..25
#   MARATHON_TOP=12              — targets per wave (ranker slices)
#   SKIP_DISCOVERY=1             — skip bounty discovery fuzz
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WAVE_FIRST="${WAVE_FIRST:-15}"
WAVE_LAST="${WAVE_LAST:-25}"
MARATHON_TOP="${MARATHON_TOP:-12}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="$ROOT/reports/bounty/oss-cve-marathon-${STAMP}"
mkdir -p "$OUT" "$ROOT/logs"

log() { echo "[marathon $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/marathon.log"; }

wait_hunt() {
  local pat="$1" label="$2"
  local pid
  pid="$(pgrep -f "$pat" | head -1 || true)"
  [[ -n "$pid" ]] || return 0
  log "wait $label pid=$pid"
  while kill -0 "$pid" 2>/dev/null; do sleep 60; done
  log "$label done"
}

log "=== OSS CVE day marathon $WAVE_FIRST..$WAVE_LAST stamp=$STAMP top=$MARATHON_TOP ==="

# Don't collide with a stray single-target hunt
wait_hunt 'oss_cve_hunt\.sh' 'existing oss_cve_hunt'
wait_hunt 'run_oss_cve_wave\.sh' 'existing wave runner'

if [[ "${SKIP_DISCOVERY:-0}" != "1" ]]; then
  log "refresh bounty program list"
  bash "$ROOT/scripts/ops/fetch_bounty_programs.sh" >>"$OUT/discovery.log" 2>&1 || log "fetch_bounty_programs skipped"
  log "discovery fuzz (light)"
  FUZZ_RUNS="${DISCOVERY_FUZZ_RUNS:-1024}" OUT="$OUT/discovery-fuzz" \
    bash "$ROOT/scripts/ops/run_bounty_discovery_fuzz.sh" >>"$OUT/discovery.log" 2>&1 || true
fi

SUMMARY="$OUT/wave_results.tsv"
echo -e "wave\trc\tverdict\tpath" >"$SUMMARY"

for WAVE in $(seq "$WAVE_FIRST" "$WAVE_LAST"); do
  WSTAMP="${STAMP}-w${WAVE}"
  TOP="$MARATHON_TOP"
  # Rotate queue width: wider early waves, tighter later sweeps
  if [[ "$WAVE" -eq 15 ]]; then TOP=14; fi
  if [[ "$WAVE" -eq 16 ]]; then TOP=16; fi
  if [[ "$WAVE" -ge 20 ]]; then TOP=10; fi

  log "--- wave $WAVE start (top=$TOP) ---"
  python3 "$ROOT/scripts/ops/rank_oss_cve_targets.py" --wave "$WAVE" --top "$TOP" \
    --out "$ROOT/reports/oss-cve/cve-rank-wave${WAVE}-${WSTAMP}.md" >>"$OUT/rank.log" 2>&1 || {
    log "WARN: rank wave $WAVE failed"
    continue
  }

  set +e
  WAVE="$WAVE" STAMP="$WSTAMP" TOP="$TOP" \
    bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$OUT/wave${WAVE}.log" 2>&1
  RC=$?
  set -e

  ROLL="$(ls -td "$ROOT/reports/oss-cve/wave${WAVE}-"*/ROLLUP.json 2>/dev/null | head -1 || true)"
  VERDICT="—"
  if [[ -n "$ROLL" ]]; then
    VERDICT="$(python3 -c "import json; print(json.load(open('$ROLL')).get('verdict','?'))" 2>/dev/null || echo '?')"
  fi
  echo -e "${WAVE}\t${RC}\t${VERDICT}\t${ROLL:-none}" >>"$SUMMARY"
  log "wave $WAVE rc=$RC verdict=$VERDICT"

  # ASAN deep chase on INFORMATIONAL targets with many crashes
  CAND="$(python3 -c "
import json, pathlib
rolls = sorted(pathlib.Path('$ROOT/reports/oss-cve').glob('wave${WAVE}-*/ROLLUP.json'))
if not rolls: raise SystemExit(0)
r = json.loads(rolls[-1].read_text())
ids = []
for t in r.get('targets', []):
    if t.get('verdict')=='INFORMATIONAL' and len(t.get('crashes') or [])>25:
        ids.append(t.get('target_id'))
print(','.join(x for x in ids if x))
" 2>/dev/null || true)"

  if [[ -n "$CAND" ]]; then
    log "wave $WAVE asan-chase: $CAND"
    WAVE="$WAVE" STAMP="${WSTAMP}-asan" \
      OUT="$ROOT/reports/oss-cve/wave${WAVE}-asan-${WSTAMP}" \
      TARGETS="$CAND" BUDGET=300000 TIME_LIMIT=7200 \
      bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$OUT/wave${WAVE}-asan.log" 2>&1 || true
  fi
done

python3 - "$OUT" "$ROOT" "$WAVE_FIRST" "$WAVE_LAST" <<'PY'
import json, pathlib, sys, time
out, root, w0, w1 = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4])
summary = {"stamp": out.name, "finished_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "waves": {}}
for w in range(w0, w1 + 1):
    rolls = sorted((root / "reports/oss-cve").glob(f"wave{w}-*/ROLLUP.json"), key=lambda p: p.stat().st_mtime)
    if not rolls:
        continue
    r = json.loads(rolls[-1].read_text())
    cve = r.get("cve_candidates") or []
    summary["waves"][str(w)] = {
        "verdict": r.get("verdict"),
        "cve_candidates": len(cve),
        "path": str(rolls[-1]),
        "summary": (r.get("summary") or "")[:200],
    }
(out / "marathon_rollup.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2))
PY

log "=== marathon done — $OUT/marathon_rollup.json ==="
cat "$SUMMARY"
