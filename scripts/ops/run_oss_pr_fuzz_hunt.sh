#!/usr/bin/env bash
# OSS PR fuzz hunt — rotate GitHub upstream guards via HackMe security-audit + pool.
#
#   bash scripts/ops/run_oss_pr_fuzz_hunt.sh
#   MAX_TARGETS=2 POOL_DIST=0 bash scripts/ops/run_oss_pr_fuzz_hunt.sh
#   TARGET_ID=dogecoin_hasvalidops bash scripts/ops/run_oss_pr_fuzz_hunt.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(tr -d '\r\n' <"${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}" 2>/dev/null || true)"
QUEUE="${QUEUE:-$ROOT/upstream/oss_pr_fuzz_queue.json}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-pr/${STAMP}}"
MAX_TARGETS="${MAX_TARGETS:-0}"
BUDGET_RUNS="${BUDGET_RUNS:-512}"
BUDGET_HMC="${BUDGET_HMC:-0.5}"
CHECK_SEMANTICS="${CHECK_SEMANTICS:-detector}"
DEPTH_TIER="${DEPTH_TIER:-wasm_native}"
POOL_DIST="${POOL_DIST:-0}"
STOP_ON_CRASH="${STOP_ON_CRASH:-1}"
POLL_TICKS="${POLL_TICKS:-450}"
SKIP_IDS="${SKIP_IDS:-}"

require_cmd curl jq xxd python3
[[ -n "$ADMIN" ]] || fail "missing admin token at .secrets/hackme_admin_token"
[[ -f "$QUEUE" ]] || fail "missing queue $QUEUE"
curl -fsS --max-time 15 "${BASE}/api/status?lite=1" >/dev/null || fail "node down at $BASE"

mkdir -p "$OUT"
log() { echo "[oss-pr $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/hunt.log"; }

log "build upstream WASM pack"
bash "$ROOT/scripts/build_upstream_l1_pack.sh" >>"$OUT/build.log" 2>&1

run_one() {
  local id="$1" repo="$2" wasm_name="$3" src="$4" title="$5" upstream_ref="$6"
  local wasm="$ROOT/tasks/artifacts/security/$wasm_name"
  [[ -f "$wasm" ]] || { log "SKIP $id — missing $wasm"; return 1; }
  go run ./tools/task_abi_check "$wasm" >>"$OUT/build.log" 2>&1 || true

  local sub_out="$OUT/$id"
  mkdir -p "$sub_out"
  local hex cid oid
  hex="$(xxd -p "$wasm" | tr -d '\n')"
  cid="campaign-osspr-${id}-${STAMP}"
  oid="order-osspr-${id}-${STAMP}"
  local pool_json="false"
  [[ "$POOL_DIST" == "1" ]] && pool_json="true"

  log "=== $id === repo=$repo"
  local tok=""
  log "POST /api/fuzz/campaigns runs=$BUDGET_RUNS pool=$pool_json"
  for attempt in 1 2 3 4 5 6 7 8 9 10; do
    set +e
    resp="$(curl -fsS --max-time 90 -X POST "${BASE}/api/fuzz/campaigns" \
      -H "Content-Type: application/json" \
      -H "X-Hackme-Admin-Token: $ADMIN" \
      -d "$(jq -nc \
        --arg id "$cid" \
        --arg title "$title" \
        --arg hex "$hex" \
        --arg sem "$CHECK_SEMANTICS" \
        --arg tier "$DEPTH_TIER" \
        --arg gname "$id" \
        --argjson runs "$BUDGET_RUNS" \
        --argjson pool "$pool_json" \
        '{
          id: $id,
          campaign_type: "fuzz",
          status: "running",
          title: $title,
          owner_ref: "oss-pr",
          budget_runs: $runs,
          budget_seconds: 3600,
          config: {
            wasm_check_hex: $hex,
            check_semantics: $sem,
            depth_tier: $tier,
            guard_name: $gname,
            native_repro_enabled: true,
            bounty_requires_native: true,
            pool_distributed: $pool,
            auto_runner: "1"
          }
        }')" 2>&1)"
    crc=$?
    set -e
    if [[ $crc -eq 0 ]] && echo "$resp" | jq -e '.customer_report_token' >/dev/null 2>&1; then
      echo "$resp" | tee "$sub_out/audit_create.json"
      tok="$(jq -r '.customer_report_token' "$sub_out/audit_create.json")"
      break
    fi
    log "  create attempt $attempt failed — sleep ${attempt}s"
    sleep "$attempt"
  done
  [[ -n "$tok" ]] || { log "WARN $id — campaign create failed"; return 1; }
  echo "$tok" >"$sub_out/report_token.txt"

  log "poll $cid (max $((POLL_TICKS * 2))s)"
  local i st done_n bud
  for i in $(seq 1 "$POLL_TICKS"); do
    curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" \
      "${BASE}/api/fuzz/campaigns/${cid}" >"$sub_out/campaign.json" || true
    st="$(jq -r '.campaign.status // "?"' "$sub_out/campaign.json" 2>/dev/null || echo "?")"
    done_n="$(jq -r '.campaign.summary.runs_done // 0' "$sub_out/campaign.json" 2>/dev/null || echo 0)"
    bud="$(jq -r '.campaign.budget_runs // 0' "$sub_out/campaign.json" 2>/dev/null || echo 0)"
    [[ $((i % 10)) -eq 0 ]] && log "  tick $i status=$st runs=$done_n/$bud"
    [[ "$st" == "completed" || "$st" == "failed" ]] && break
    [[ "$bud" -gt 0 && "$done_n" -ge "$bud" ]] && break
    sleep 2
  done

  curl -fsS --max-time 60 -H "X-Hackme-Report-Token: $tok" \
    "${BASE}/api/fuzz/campaigns/${cid}/report?format=json&limit=120" \
    | jq . >"$sub_out/report.json" || echo '{}' >"$sub_out/report.json"
  curl -fsS --max-time 60 -H "X-Hackme-Report-Token: $tok" \
    "${BASE}/api/fuzz/campaigns/${cid}/report.html?limit=120" \
    -o "$sub_out/report.html" 2>/dev/null || true

  python3 - "$sub_out" "$id" "$repo" "$upstream_ref" "$title" <<'PY'
import json, pathlib, sys
sub, tid, repo, ref, title = sys.argv[1:6]
sub = pathlib.Path(sub)
rep = json.loads((sub / "report.json").read_text()) if (sub / "report.json").exists() else {}
tot = rep.get("totals") or {}
by_sev = tot.get("by_severity") or {}
findings = rep.get("findings") or []
crashes = [f for f in findings if (f.get("detail") or {}).get("triage_class") in ("needs_triage", "crash")]
guards = [f for f in findings if (f.get("detail") or {}).get("triage_class") == "guard_signal"]
crit = int(by_sev.get("critical", 0) or 0)
high = int(by_sev.get("high", 0) or 0)
pr_ready = crit > 0 or len(crashes) > 0
summary = {
    "target_id": tid,
    "repo": repo,
    "upstream_ref": ref,
    "title": title,
    "verdict": rep.get("verdict"),
    "critical": crit,
    "high": high,
    "guard_signals": len(guards),
    "crash_candidates": len(crashes),
    "pr_ready": pr_ready,
    "pr_angle": (
        "potential finding — native repro required before disclosure"
        if pr_ready else
        "clean guard sweep — publish as security research / fuzz coverage post"
    ),
    "report_html": str(sub / "report.html"),
}
(sub / "TARGET_SUMMARY.json").write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary))
PY
  local pr_ready
  pr_ready="$(jq -r '.pr_ready' "$sub_out/TARGET_SUMMARY.json")"
  if [[ "$pr_ready" == "true" && "$STOP_ON_CRASH" == "1" ]]; then
    log "STOP — crash/critical candidate on $id"
    return 2
  fi
  log "next target (guard-only on $id)"
  return 0
}

log "start OUT=$OUT max_targets=$MAX_TARGETS"
: >"$OUT/results.jsonl"
count=0
while IFS= read -r row; do
  id="$(echo "$row" | jq -r '.id')"
  [[ -n "${TARGET_ID:-}" && "$id" != "$TARGET_ID" ]] && continue
  if [[ -n "$SKIP_IDS" ]]; then
    case ",$SKIP_IDS," in *,"$id",*) log "skip $id (SKIP_IDS)"; continue ;; esac
  fi
  repo="$(echo "$row" | jq -r '.repo')"
  wasm="$(echo "$row" | jq -r '.wasm')"
  src="$(echo "$row" | jq -r '.source')"
  title="$(echo "$row" | jq -r '.title')"
  ref="$(echo "$row" | jq -r '.upstream_ref')"
  set +e
  run_one "$id" "$repo" "$wasm" "$src" "$title" "$ref"
  rc=$?
  set -e
  [[ -f "$OUT/$id/TARGET_SUMMARY.json" ]] && jq -c . "$OUT/$id/TARGET_SUMMARY.json" >>"$OUT/results.jsonl"
  count=$((count + 1))
  [[ "$MAX_TARGETS" -gt 0 && "$count" -ge "$MAX_TARGETS" ]] && break
  [[ "$rc" -eq 2 ]] && break
done < <(jq -c '.targets[]' "$QUEUE")

python3 - "$OUT" <<'PY'
import json, pathlib, sys, time
out = pathlib.Path(sys.argv[1])
rows = []
for line in (out / "results.jsonl").read_text().splitlines():
    if line.strip():
        rows.append(json.loads(line))
pr_hits = [r for r in rows if r.get("pr_ready")]
rollup = {
    "stamp": out.name,
    "time_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "targets_run": len(rows),
    "pr_candidates": pr_hits,
    "verdict": "PR_CANDIDATE" if pr_hits else "ROTATE_CLEAN",
    "targets": rows,
}
(out / "rollup.json").write_text(json.dumps(rollup, indent=2) + "\n")
print(json.dumps(rollup, indent=2))
PY

ln -sfn "$OUT" "$ROOT/reports/oss-pr/CURRENT"
log "native repro gate"
if bash "$ROOT/scripts/ops/native_repro_oss_guard.sh" "$OUT" >>"$OUT/hunt.log" 2>&1; then
  log "native_verdict: publish_allowed (methodology case study only — no CVE)"
  if [[ "${EXPORT_SITE:-1}" == "1" ]]; then
    python3 "$ROOT/scripts/ops/export_oss_pr_rollup_html.py" "$OUT" >>"$OUT/hunt.log" 2>&1 \
      && log "site export → web/site/reports/oss-pr-sweep/" \
      || log "WARN site export skipped (export error)"
  fi
elif [[ -f "$OUT/native_verdict.json" ]]; then
  log "BLOCK site publish — native CVE candidate; contact maintainer before public case study"
  jq '. + {"publish_blocked": true, "publish_reason": "native_repro_cve_candidate"}' \
    "$OUT/rollup.json" >"$OUT/rollup.tmp" && mv "$OUT/rollup.tmp" "$OUT/rollup.json"
else
  log "WARN native repro script failed — see hunt.log (no site export)"
fi
log "done → $OUT/rollup.json + $OUT/native_verdict.json"
