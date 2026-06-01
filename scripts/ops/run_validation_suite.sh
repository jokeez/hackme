#!/usr/bin/env bash
# Full validation matrix: miner WOW path, red-team, customer/fuzz/lang gates.
# Default: SAFE alongside overnight monitor + desktop_morning_report (no reset, no :8080 takeover).
#
# Usage (repo root):
#   bash scripts/ops/run_validation_suite.sh
#   TIER=full bash scripts/ops/run_validation_suite.sh    # + local orders, go test, fuzz escrow
#   TIER=ephemeral bash scripts/ops/run_validation_suite.sh  # isolated ports (slow, ~15–30 min)
#   bash scripts/tests/miner_journey_wow.sh                 # miner path only
#
# Morning (unchanged):
#   bash scripts/ops/desktop_morning_report.sh reports/overnight/CURRENT
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
TIER="${TIER:-safe}"
OUT="$ROOT/reports/validation-suite-$STAMP"
mkdir -p "$OUT/jobs"
VERDICT="$OUT/VERDICT.md"

ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"
if [[ -z "$ADMIN_TOKEN" ]] && [[ -f "$ROOT/.env.desktop" ]]; then
  ADMIN_TOKEN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$ROOT/.env.desktop" 2>/dev/null | cut -d= -f2- || true)"
fi
export ADMIN_TOKEN HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN_TOKEN}"
BASE_LOCAL="${BASE_LOCAL:-http://127.0.0.1:8080}"
BASE_PROD="${BASE_PROD:-https://hackme.tech}"
COORD_PROD="${COORD_PROD:-${BASE_PROD}/pool/coordinator}"

log() { echo "[validation] $*" | tee -a "$OUT/orchestrator.log"; }

overnight_running() {
  pgrep -f 'desktop_overnight_monitor\.sh' >/dev/null 2>&1
}

if overnight_running || [[ "${SAFE_FOR_OVERNIGHT:-auto}" == "1" ]]; then
  export SAFE_FOR_OVERNIGHT=1
  export SKIP_BOOTSTRAP=1
  export SKIP_LIVE_MINER_START=1
  export SKIP_WORKER_SMOKE=1
  export SKIP_WORKER_RESET=1
  log "SAFE_FOR_OVERNIGHT=1 (overnight monitor detected — will not reset worker or start :8080 miner)"
fi

if [[ -f "$ROOT/reports/overnight/CURRENT/summary.json" ]]; then
  log "overnight summary exists — morning report ready at reports/overnight/CURRENT/summary.json"
fi

run_job() {
  local id="$1"
  shift
  local jlog="$OUT/jobs/${id}.log"
  local jexit="$OUT/jobs/${id}.exit"
  log "start job $id"
  chmod +x "$ROOT/scripts/ops/_validation_job_wrapper.sh"
  setsid nohup "$ROOT/scripts/ops/_validation_job_wrapper.sh" "$jlog" "$jexit" "$@" \
    >>"$OUT/nohup.log" 2>&1 &
  echo $! >"$OUT/jobs/${id}.pid"
  disown 2>/dev/null || true
}

run_job_sync() {
  local id="$1"
  shift
  local jlog="$OUT/jobs/${id}.log"
  local jexit="$OUT/jobs/${id}.exit"
  log "run sync $id"
  if "$@" >"$jlog" 2>&1; then
    echo 0 >"$jexit"
  else
    echo $? >"$jexit"
  fi
}

log "=== validation suite tier=$TIER stamp=$STAMP ==="
log "out=$OUT"

chmod +x "$ROOT/scripts/tests/miner_journey_wow.sh" \
  "$ROOT/scripts/ops/penny_reconcile.sh" 2>/dev/null || true

# --- Miner WOW (sync first — headline path) ---
run_job_sync "miner_journey_wow" \
  env SITE_BASE="$BASE_PROD" SKIP_LIVE_MINER_START="${SKIP_LIVE_MINER_START:-1}" SKIP_WORKER_SMOKE="${SKIP_WORKER_SMOKE:-1}" \
  OUT_DIR="$OUT/miner-journey-wow" bash "$ROOT/scripts/tests/miner_journey_wow.sh"

# --- Parallel safe jobs (prod read-only + static + isolated HMS) ---
run_job "public_site" bash "$ROOT/scripts/tests/public_site_smoke.sh"
run_job "new_miner_gate" env BASE="$BASE_PROD" COORD_URL="$COORD_PROD" WORKER_SMOKE=0 \
  OUT_DIR="$OUT/new-miner-gate" bash "$ROOT/scripts/ops/new_miner_journey_gate.sh"
run_job "fuzz_dev_portal" env BASE="$BASE_PROD" bash "$ROOT/scripts/tests/fuzzing_developer_portal_smoke.sh"
run_job "fuzz_public_hardening" env BASE="$BASE_PROD" bash "$ROOT/scripts/tests/fuzzing_public_hardening_smoke.sh"
run_job "redteam_prod" env BASE="$BASE_PROD" bash "$ROOT/scripts/tests/redteam_surface_smoke.sh"
run_job "lang_static" env STATIC_ONLY=1 RUN_ID="val_${STAMP}" bash "$ROOT/scripts/tests/run_language_production_pack.sh"
run_job "l1_v4_gate" bash "$ROOT/scripts/ops/l1_crypto_stack_v4_gate.sh"
run_job "l1_v3_gate" bash "$ROOT/scripts/ops/l1_crypto_stack_v3_gate.sh"
run_job "l1_v2_gate" bash "$ROOT/scripts/ops/l1_crypto_stack_gate.sh"
run_job "fuzz_escrow_redteam" bash "$ROOT/scripts/ops/pool_fuzz_escrow_redteam_gate.sh"
run_job "hms_market_redteam" bash "$ROOT/scripts/tests/hms_market_redteam.sh"

if curl -fsS --max-time 5 "${BASE_LOCAL}/api/status?lite=1" >/dev/null 2>&1; then
  run_job "redteam_local" env BASE="$BASE_LOCAL" bash "$ROOT/scripts/tests/redteam_surface_smoke.sh"
  if [[ -n "$ADMIN_TOKEN" ]]; then
    run_job "economics_confidence" env DESKTOP_ENV_FILE="$ROOT/.env.desktop" \
      bash "$ROOT/scripts/tests/economics_confidence_gate.sh"
    run_job "hardware_tab_gate" env DESKTOP_ENV_FILE="$ROOT/.env.desktop" \
      bash "$ROOT/scripts/tests/hardware_tab_gate.sh"
    run_job "fuzz_dashboard_local" env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN_TOKEN" \
      bash "$ROOT/scripts/tests/fuzz_dashboard_smoke.sh"
    run_job "security_local" env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN_TOKEN" \
      bash "$ROOT/scripts/tests/security_assertions.sh"
  else
    log "skip local fuzz/security (no ADMIN_TOKEN)"
    for id in fuzz_dashboard_local security_local; do
      : >"$OUT/jobs/${id}.skip"
      echo "[validation] SKIP $id (no ADMIN_TOKEN)" >"$OUT/jobs/${id}.log"
    done
  fi
else
  log "skip local API jobs (no :8080)"
  for id in redteam_local fuzz_dashboard_local security_local; do
    : >"$OUT/jobs/${id}.skip"
    echo "[validation] SKIP $id" >"$OUT/jobs/${id}.log"
  done
fi

if [[ "${INTEGRATOR_SMOKE_PROD:-0}" == "1" ]]; then
  run_job "integrator_prod" env BASE="$BASE_PROD" bash "$ROOT/scripts/tests/integrator_self_service_smoke.sh"
fi

if [[ "$TIER" == "full" || "$TIER" == "ephemeral" ]]; then
  run_job "go_test_short" go test -short -count=1 ./... -timeout=300s
  if [[ -n "$ADMIN_TOKEN" ]] && curl -fsS --max-time 5 "${BASE_LOCAL}/api/status?lite=1" >/dev/null 2>&1; then
    run_job "orders_matrix" env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="val_${STAMP}" \
      bash "$ROOT/scripts/tests/orders_matrix.sh"
    run_job "transfers_matrix" env BASE="$BASE_LOCAL" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="val_${STAMP}" \
      bash "$ROOT/scripts/tests/transfers_matrix.sh"
  fi
fi

if [[ "$TIER" == "ephemeral" ]]; then
  run_job "ephemeral_gates" env SKIP_HEAVY_FUZZ=1 bash "$ROOT/scripts/ops/run_ephemeral_stack_and_gates.sh"
fi

log "waiting for jobs..."
for pidfile in "$OUT/jobs"/*.pid; do
  [[ -f "$pidfile" ]] || continue
  pid="$(cat "$pidfile")"
  wait "$pid" 2>/dev/null || true
done
# Ensure every background job wrote its exit file (wrapper can lag behind wait).
for _ in $(seq 1 120); do
  pending=0
  for pidfile in "$OUT/jobs"/*.pid; do
    [[ -f "$pidfile" ]] || continue
    id="$(basename "$pidfile" .pid)"
    [[ -f "$OUT/jobs/${id}.exit" ]] || pending=$((pending + 1))
  done
  [[ "$pending" -eq 0 ]] && break
  sleep 1
done

python3 - "$OUT" "$VERDICT" "$STAMP" "$TIER" <<'PY'
import json
from pathlib import Path
import sys

out = Path(sys.argv[1])
verdict = Path(sys.argv[2])
stamp = sys.argv[3]
tier = sys.argv[4]

rows = []
pass_n = fail_n = skip_n = 0
for skip_f in sorted(out.glob("jobs/*.skip")):
    job = skip_f.stem
    if (out / "jobs" / f"{job}.exit").exists():
        continue
    status = "SKIP"
    skip_n += 1
    tail = ""
    logf = out / "jobs" / f"{job}.log"
    if logf.exists():
        lines = logf.read_text(errors="replace").splitlines()
        tail = lines[-1][:140] if lines else ""
    rows.append((job, status, tail))

for exit_f in sorted(out.glob("jobs/*.exit")):
    job = exit_f.stem
    if (out / "jobs" / f"{job}.skip").exists():
        status = "SKIP"
        skip_n += 1
        tail = ""
        logf = out / "jobs" / f"{job}.log"
        if logf.exists():
            lines = logf.read_text(errors="replace").splitlines()
            tail = lines[-1][:140] if lines else ""
        rows.append((job, status, tail))
        continue
    raw = exit_f.read_text().strip()
    if not raw:
        continue
    code = int(raw)
    if code == 0:
        status = "PASS"
        pass_n += 1
    else:
        status = "FAIL"
        fail_n += 1
    tail = ""
    logf = out / "jobs" / f"{job}.log"
    if logf.exists():
        lines = logf.read_text(errors="replace").splitlines()
        tail = lines[-1][:140] if lines else ""
    rows.append((job, status, tail))

wow_md = out / "miner-journey-wow" / "REPORT.md"
wow_verdict = ""
if wow_md.exists():
    for line in wow_md.read_text().splitlines():
        if "Verdict:" in line:
            wow_verdict = line.strip()
            break

overall = "GO"
if fail_n > 0:
    flaky = [j for j, st, _ in rows if st == "FAIL" and j == "public_site"]
    if fail_n == 1 and flaky:
        overall = "WARN"
    else:
        overall = "WARN" if fail_n <= 2 else "NO-GO"

lines = [
    f"# Validation suite — {stamp}",
    "",
    f"**Tier:** `{tier}` · **Jobs:** {pass_n} pass / {fail_n} fail / {skip_n} skip",
    f"**Miner WOW:** {wow_verdict or '(see miner-journey-wow/REPORT.md)'}",
    "",
    "Does **not** modify `reports/overnight/CURRENT`. Morning:",
    "`bash scripts/ops/desktop_morning_report.sh reports/overnight/CURRENT`",
    "",
    "| Job | Result | Last line |",
    "|-----|--------|-----------|",
]
for job, st, tail in rows:
    lines.append(f"| {job} | **{st}** | `{tail}` |")
lines.append("")
lines.append(f"## Verdict: **{overall}**")
verdict.write_text("\n".join(lines) + "\n")
print("\n".join(lines))
PY

log "done — $VERDICT"
cat "$VERDICT"
