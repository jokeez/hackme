#!/usr/bin/env bash
# Obscure / low-coverage OSS CVE hunt — small & newer C parsers (wave27 tier).
#
#   bash scripts/ops/run_oss_cve_obscure.sh
#   BUDGET=500000 TIME_LIMIT=7200 bash scripts/ops/run_oss_cve_obscure.sh
#   OBSCURE_PARALLEL=2 bash scripts/ops/run_oss_cve_obscure.sh
#
# Each target gets the full BUDGET + TIME_LIMIT (no shared wall unless HACKME_OSS_SHARE_WALL=1).
# Targets: frozen, mpack, libcbor, kuba_zip, cwalk, jsonparser, tinycbor, microtar
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

OBSCURE_TARGETS="${OBSCURE_TARGETS:-frozen,mpack,libcbor,kuba_zip,cwalk,jsonparser,tinycbor,microtar,xchange}"
BUDGET="${BUDGET:-300000}"
TIME_LIMIT="${TIME_LIMIT:-7200}"
OBSCURE_PARALLEL="${OBSCURE_PARALLEL:-2}"
L2_PREFLIGHT="${L2_PREFLIGHT:-1}"
L2_WALL_SEC="${L2_WALL_SEC:-45}"
# Prefer deep_v1 when shallow CLEAN-saturated (mpack/libcbor/… have deep_driver).
export HACKME_OSS_HARNESS_VARIANT="${HACKME_OSS_HARNESS_VARIANT:-deep_v1}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/obscure-${STAMP}}"
# Customer pool dig seed feed stays off unless explicitly enabled.
export HACKME_POOL_SEED_FROM_RESEARCH="${HACKME_POOL_SEED_FROM_RESEARCH:-0}"

mkdir -p "$OUT" "$ROOT/logs"
log() { echo "[obscure $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/obscure.log"; }

log "=== OSS obscure hunt targets=$OBSCURE_TARGETS budget=$BUDGET time=${TIME_LIMIT}s/target parallel=$OBSCURE_PARALLEL ==="

SUMMARY="$OUT/target_summary.tsv"
echo -e "target\trc\tverdict\titers\tcrashes\tasan\treport" >"$SUMMARY"

IFS=',' read -r -a IDS <<< "$OBSCURE_TARGETS"
OVERALL_RC=0
CVE_ANY=0

# Best-effort L2 / libFuzzer seed import before deep mutation (does not fail the hunt).
if [[ "$L2_PREFLIGHT" == "1" ]] && [[ -f "$ROOT/scripts/ops/hunt_import_libfuzzer_corpus.sh" ]]; then
  for TID in "${IDS[@]}"; do
    TID="$(echo "$TID" | tr -d '[:space:]')"
    [[ -z "$TID" ]] && continue
    log "L2 preflight TARGET=$TID wall=${L2_WALL_SEC}s"
    set +e
    TARGET="$TID" WALL_SEC="$L2_WALL_SEC" bash "$ROOT/scripts/ops/hunt_import_libfuzzer_corpus.sh" >>"$OUT/obscure.log" 2>&1
    set -e
  done
fi

# One shared pack build so parallel workers skip redundant compiles.
log "preflight: build OSS CVE pack for obscure targets"
set +e
TARGETS="$OBSCURE_TARGETS" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build.log" 2>&1
set -e

run_one() {
  local TID="$1"
  local TOUT="$OUT/$TID"
  mkdir -p "$TOUT"
  log "--- $TID start ---"
  set +e
  # Full per-target wall (do not share unless operator sets HACKME_OSS_SHARE_WALL=1).
  TARGETS="$TID" BUDGET="$BUDGET" TIME_LIMIT="$TIME_LIMIT" STAMP="${STAMP}-${TID}" OUT="$TOUT" \
    SKIP_PACK_BUILD=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$OUT/obscure.log" 2>&1
  local RC=$?
  set -e

  local VERDICT="—" ITERS=0 CRASHES=0 ASAN=0
  local REPORT="$TOUT/$TID/HUNT_REPORT.json"
  if [[ -f "$TOUT/ROLLUP.json" ]]; then
    read -r VERDICT ITERS CRASHES ASAN < <(python3 -c "
import json
r=json.load(open('$TOUT/ROLLUP.json'))
t=(r.get('targets') or [{}])[0]
cr=t.get('crashes') or []
asan=sum(1 for c in cr if (c.get('sanitizer_class')=='asan') or 'AddressSanitizer' in (c.get('sanitizer') or ''))
print(r.get('verdict','?'), t.get('iterations',0), len(cr), asan)
") || true
  fi
  # Serialize summary writes when parallel.
  (
    flock 9
    echo -e "${TID}\t${RC}\t${VERDICT}\t${ITERS}\t${CRASHES}\t${ASAN}\t${REPORT}" >>"$SUMMARY"
  ) 9>>"$OUT/.summary.lock"
  log "$TID done rc=$RC verdict=$VERDICT iters=$ITERS crashes=$CRASHES asan=$ASAN"
  # Persist rc for parent aggregation
  echo "$RC" >"$TOUT/.rc"
  echo "$VERDICT" >"$TOUT/.verdict"
  return 0
}

if ! [[ "$OBSCURE_PARALLEL" =~ ^[0-9]+$ ]] || (( OBSCURE_PARALLEL < 1 )); then
  OBSCURE_PARALLEL=1
fi

if (( OBSCURE_PARALLEL == 1 )); then
  for TID in "${IDS[@]}"; do
    TID="$(echo "$TID" | tr -d '[:space:]')"
    [[ -z "$TID" ]] && continue
    run_one "$TID"
  done
else
  log "parallel workers=$OBSCURE_PARALLEL"
  pids=()
  for TID in "${IDS[@]}"; do
    TID="$(echo "$TID" | tr -d '[:space:]')"
    [[ -z "$TID" ]] && continue
    while (( $(jobs -rp | wc -l) >= OBSCURE_PARALLEL )); do
      sleep 2
    done
    run_one "$TID" &
    pids+=("$!")
  done
  for pid in "${pids[@]+"${pids[@]}"}"; do
    wait "$pid" || true
  done
fi

# Aggregate overall rc / CVE
for TID in "${IDS[@]}"; do
  TID="$(echo "$TID" | tr -d '[:space:]')"
  [[ -z "$TID" ]] && continue
  RC=0
  VERDICT="—"
  [[ -f "$OUT/$TID/.rc" ]] && RC="$(cat "$OUT/$TID/.rc")"
  [[ -f "$OUT/$TID/.verdict" ]] && VERDICT="$(cat "$OUT/$TID/.verdict")"
  [[ $RC -eq 0 || $RC -eq 1 ]] || OVERALL_RC=$RC
  [[ "$VERDICT" == "CVE_CANDIDATE" ]] && CVE_ANY=1
done

python3 - "$OUT" <<'PY' | tee -a "$OUT/obscure.log"
import json, pathlib, sys
out = pathlib.Path(sys.argv[1])
rows = []
for line in (out / "target_summary.tsv").read_text().splitlines()[1:]:
    if not line.strip():
        continue
    tid, rc, verdict, iters, crashes, asan, report = line.split("\t")
    rows.append(dict(target=tid, rc=int(rc), verdict=verdict, iterations=int(iters),
                     crashes=int(crashes), asan=int(asan), report=report))
rollup = {
    "wave": "obscure27",
    "strategy": "parallel_full_budget_dict_corpus",
    "targets": rows,
    "verdict": "CVE_CANDIDATE" if any(r["verdict"] == "CVE_CANDIDATE" for r in rows) else "CLEAN",
}
(out / "OBSCURE_ROLLUP.json").write_text(json.dumps(rollup, indent=2) + "\n")
print("wrote", out / "OBSCURE_ROLLUP.json")
for r in rows:
    print(f"  {r['target']}: {r['verdict']} iters={r['iterations']} asan={r['asan']}")
PY

ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT-obscure"
log "=== obscure hunt complete overall_rc=$OVERALL_RC cve_any=$CVE_ANY out=$OUT ==="
[[ $CVE_ANY -eq 1 ]] && exit 1
exit "$OVERALL_RC"
