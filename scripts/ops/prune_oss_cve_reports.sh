#!/usr/bin/env bash
# Reclaim disk from reports/oss-cve — keeps rollups + recent runs, strips UBSan crash dumps.
#
#   bash scripts/ops/prune_oss_cve_reports.sh           # execute
#   DRY_RUN=1 bash scripts/ops/prune_oss_cve_reports.sh # preview
#
# Env:
#   KEEP_WAVE_MIN=43     — delete entire wave dirs with wave number < this (if CLEAN)
#   KEEP_DAYS=2          — never strip/delete runs newer than N days
#   DRY_RUN=1            — print actions only
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPORT_ROOT="$ROOT/reports/oss-cve"
KEEP_WAVE_MIN="${KEEP_WAVE_MIN:-43}"
KEEP_DAYS="${KEEP_DAYS:-2}"
DRY_RUN="${DRY_RUN:-0}"
NOW_EPOCH="$(date +%s)"
CUTOFF_EPOCH=$((NOW_EPOCH - KEEP_DAYS * 86400))

log() { echo "[prune $(date -u +%H:%M:%S)] $*"; }
run() {
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY: $*"
  else
    log "$*"
    eval "$@"
  fi
}

has_cve_candidate() {
  local roll="$1"
  python3 - "$roll" <<'PY' 2>/dev/null
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
if not p.is_file():
    raise SystemExit(1)
r = json.loads(p.read_text())
if r.get("verdict") == "CVE_CANDIDATE":
    raise SystemExit(0)
if r.get("cve_candidates"):
    raise SystemExit(0)
raise SystemExit(1)
PY
}

dir_mtime_epoch() {
  stat -c %Y "$1" 2>/dev/null || echo 0
}

wave_num_from_name() {
  local name="$1"
  if [[ "$name" =~ wave([0-9]+) ]]; then
    echo "${BASH_REMATCH[1]}"
    return 0
  fi
  echo 999
}

bytes_before="$(du -sb "$REPORT_ROOT" 2>/dev/null | awk '{print $1}')"
stripped_crash=0
deleted_dirs=0
trimmed_reports=0

log "start keep_wave>=$KEEP_WAVE_MIN keep_days=$KEEP_DAYS dry=$DRY_RUN"
log "disk before: $(du -sh "$REPORT_ROOT" | awk '{print $1}')"

# --- 1) Strip crash corpora from non-CVE runs ---
while IFS= read -r -d '' roll; do
  run_dir="$(dirname "$roll")"
  mtime="$(dir_mtime_epoch "$run_dir")"
  if [[ "$mtime" -gt "$CUTOFF_EPOCH" ]] && [[ "${FORCE_STRIP_RECENT:-0}" != "1" ]]; then
    continue
  fi
  if has_cve_candidate "$roll"; then
    log "keep crashes (CVE_CANDIDATE): $run_dir"
    continue
  fi
  while IFS= read -r -d '' crash_dir; do
    sz="$(du -sb "$crash_dir" 2>/dev/null | awk '{print $1}')"
    stripped_crash=$((stripped_crash + sz))
    run "rm -rf '$crash_dir'"
  done < <(find "$run_dir" -type d -name crashes -print0 2>/dev/null)

  while IFS= read -r -d '' hr; do
    sz="$(stat -c %s "$hr" 2>/dev/null || echo 0)"
    if [[ "$sz" -gt 5000000 ]]; then
      trimmed_reports=$((trimmed_reports + sz))
      if [[ "$DRY_RUN" == "1" ]]; then
        log "DRY: truncate $(basename "$hr") in $run_dir (${sz} bytes)"
      else
        python3 - "$hr" <<'PY'
import json, pathlib, sys
p = pathlib.Path(sys.argv[1])
try:
    r = json.loads(p.read_text())
except Exception:
    raise SystemExit(0)
keep = {
    "target_id": r.get("target_id"),
    "verdict": r.get("verdict"),
    "iterations": r.get("iterations"),
    "crash_count": r.get("crash_count"),
    "asan_crashes": r.get("asan_crashes"),
    "summary": (r.get("summary") or "")[:500],
    "pruned": True,
}
p.write_text(json.dumps(keep, indent=2) + "\n")
PY
        log "trimmed $(basename "$hr") in $run_dir"
      fi
    fi
  done < <(find "$run_dir" -maxdepth 2 -name HUNT_REPORT.json -print0 2>/dev/null)
done < <(find "$REPORT_ROOT" -maxdepth 2 -name ROLLUP.json -print0 2>/dev/null)

# --- 2) Delete old CLEAN wave dirs (number < KEEP_WAVE_MIN) ---
for dir in "$REPORT_ROOT"/wave* "$REPORT_ROOT"/hold-deep-* "$REPORT_ROOT"/easy-yield-*; do
  [[ -d "$dir" ]] || continue
  base="$(basename "$dir")"
  [[ "$base" == CURRENT* ]] && continue
  wn="$(wave_num_from_name "$base")"
  if [[ "$wn" -ge "$KEEP_WAVE_MIN" ]]; then
    continue
  fi
  mtime="$(dir_mtime_epoch "$dir")"
  if [[ "$mtime" -gt "$CUTOFF_EPOCH" ]]; then
    continue
  fi
  roll="$dir/ROLLUP.json"
  if [[ -f "$roll" ]] && has_cve_candidate "$roll"; then
    log "keep old wave (CVE): $base"
    continue
  fi
  sz="$(du -sb "$dir" 2>/dev/null | awk '{print $1}')"
  deleted_dirs=$((deleted_dirs + sz))
  run "rm -rf '$dir'"
done

bytes_after="$(du -sb "$REPORT_ROOT" 2>/dev/null | awk '{print $1}')"
freed=$((bytes_before - bytes_after))
log "done — freed ~$((freed / 1048576)) MiB (crashes ~$((stripped_crash / 1048576)) MiB, dirs ~$((deleted_dirs / 1048576)) MiB)"
log "disk after: $(du -sh "$REPORT_ROOT" | awk '{print $1}') · $(df -h "$ROOT" | tail -1)"
