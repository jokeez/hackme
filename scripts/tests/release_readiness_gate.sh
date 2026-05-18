#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd jq

OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/readiness"
ensure_reports_dir "$OUT"

FULL_RUN_ID="${FULL_RUN_ID:-}"
ADV_RUN_ID="${ADV_RUN_ID:-}"
PRE_RUN_ID="${PRE_RUN_ID:-}"
MEGA_RUN_ID="${MEGA_RUN_ID:-}"

record_fail() {
  local msg="$1"
  jq -nc --arg verdict "fail" --arg message "$msg" '{verdict:$verdict,message:$message}' >>"$OUT/results.jsonl"
}

record_pass() {
  local msg="$1"
  jq -nc --arg verdict "pass" --arg message "$msg" '{verdict:$verdict,message:$message}' >>"$OUT/results.jsonl"
}

read_status() {
  local run_id="$1"
  local file="$OUT_DIR/$run_id/summary_all.json"
  if [[ ! -f "$file" ]]; then
    echo "MISSING"
    return
  fi
  jq -r '.status // "UNKNOWN"' "$file" 2>/dev/null || echo "UNKNOWN"
}

has_summary_artifacts() {
  local run_id="$1"
  local file="$OUT_DIR/$run_id/summary_all.json"
  [[ -f "$file" ]] || return 1
  local suites total
  suites="$(jq -r '.suites | length // 0' "$file" 2>/dev/null || echo 0)"
  total="$(jq -r '.total_cases // 0' "$file" 2>/dev/null || echo 0)"
  [[ "$suites" -ge 1 && "$total" -ge 1 ]]
}

has_required_suite() {
  local run_id="$1"
  local suite="$2"
  local file="$OUT_DIR/$run_id/summary_all.json"
  [[ -f "$file" ]] || return 1
  jq -e --arg s "$suite" 'any(.suites[]?.path; tostring | test("/"+$s+"/summary.json$"))' "$file" >/dev/null 2>&1
}

resolve_latest_by_suffix() {
  local suffix="$1"
  local cand=""
  local d b
  for d in "$OUT_DIR"/*; do
    [[ -d "$d" ]] || continue
    b="$(basename "$d")"
    if [[ "$b" == *"$suffix" ]]; then
      cand="$b"
    fi
  done
  printf '%s' "$cand"
}

: >"$OUT/results.jsonl"

if [[ -z "$FULL_RUN_ID" ]]; then FULL_RUN_ID="$(resolve_latest_by_suffix "_full")"; fi
if [[ -z "$ADV_RUN_ID" ]]; then ADV_RUN_ID="$(resolve_latest_by_suffix "_adv")"; fi
if [[ -z "$PRE_RUN_ID" ]]; then PRE_RUN_ID="$(resolve_latest_by_suffix "_pre")"; fi
if [[ -z "$MEGA_RUN_ID" ]]; then MEGA_RUN_ID="$(resolve_latest_by_suffix "_mega")"; fi

fails=0

for pair in "full:$FULL_RUN_ID" "adv:$ADV_RUN_ID" "pre:$PRE_RUN_ID" "mega:$MEGA_RUN_ID"; do
  gate="${pair%%:*}"
  rid="${pair#*:}"
  if [[ -z "$rid" ]]; then
    record_fail "$gate gate run id is not set"
    fails=$((fails+1))
    continue
  fi
  st="$(read_status "$rid")"
  if ! has_summary_artifacts "$rid"; then
    record_fail "$gate gate missing required summary artifacts ($rid)"
    fails=$((fails+1))
    continue
  fi
  if [[ "$gate" == "pre" ]] && ! has_required_suite "$rid" "soak"; then
    record_fail "$gate gate missing soak suite artifact ($rid)"
    fails=$((fails+1))
    continue
  fi
  if [[ "$st" == "PASS" ]]; then
    record_pass "$gate gate PASS ($rid)"
  else
    record_fail "$gate gate not PASS ($rid status=$st)"
    fails=$((fails+1))
  fi
done

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg full_run_id "$FULL_RUN_ID" \
  --arg adv_run_id "$ADV_RUN_ID" \
  --arg pre_run_id "$PRE_RUN_ID" \
  --arg mega_run_id "$MEGA_RUN_ID" \
  --argjson fails "$fails" \
  '{
    run_id:$run_id,
    captured_at:$captured_at,
    gates:{
      full:$full_run_id,
      adv:$adv_run_id,
      pre_release:$pre_run_id,
      mega:$mega_run_id
    },
    readiness:(if $fails==0 then "READY_FOR_PRIVATE_EXPANSION" else "NOT_READY" end),
    fails:$fails
  }' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "release readiness: NOT_READY ($fails gate failures). See $OUT"
fi
pass "release readiness: READY_FOR_PRIVATE_EXPANSION. See $OUT"

