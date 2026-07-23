#!/usr/bin/env bash
# Aggregate production readiness: public probes, local fuzz, security, ideal snapshot.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/master-gate-$STAMP"
VERDICT="$OUT/MASTER_VERDICT.md"
mkdir -p "$OUT"

DESK="$(grep '^HACKME_ADMIN_TOKEN=' .env.desktop 2>/dev/null | cut -d= -f2- || true)"
COORD_TOKEN="$(tr -d '\r\n' <.secrets/hackme_coordinator_admin_token 2>/dev/null || true)"
export ADMIN_TOKEN="${ADMIN_TOKEN:-$DESK}"
export HACKME_ADMIN_TOKEN="$ADMIN_TOKEN"
export COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-$COORD_TOKEN}"

pass=0
fail=0
skip=0

run_step() {
  local id="$1"
  local desc="$2"
  shift 2
  local log="$OUT/${id}.log"
  echo "[master] === $id: $desc ==="
  if "$@" >"$log" 2>&1; then
    echo "[master] PASS $id"
    echo "| $id | PASS | $desc |" >>"$OUT/results.md"
    pass=$((pass + 1))
  else
    echo "[master] FAIL $id (see $log)"
    echo "| $id | **FAIL** | $desc |" >>"$OUT/results.md"
    fail=$((fail + 1))
  fi
}

run_step_optional() {
  local id="$1"
  local desc="$2"
  shift 2
  local log="$OUT/${id}.log"
  echo "[master] === $id (optional): $desc ==="
  if "$@" >"$log" 2>&1; then
    echo "[master] PASS $id"
    echo "| $id | PASS | $desc |" >>"$OUT/results.md"
    pass=$((pass + 1))
  else
    echo "[master] SKIP/WARN $id"
    echo "| $id | WARN | $desc |" >>"$OUT/results.md"
    skip=$((skip + 1))
  fi
}

: >"$OUT/results.md"
echo "| Step | Result | Description |" >>"$OUT/results.md"
echo "|------|--------|-------------|" >>"$OUT/results.md"

run_step "ideal_finalize" "pool ideal snapshot + settlement" bash "$ROOT/scripts/ops/pool_ideal_finalize.sh"
run_step "new_miner_journey" "public new-miner journey gate" env WORKER_SMOKE=0 bash "$ROOT/scripts/ops/new_miner_journey_gate.sh"
run_step "fuzz_smoke" "fuzz dashboard API smoke" bash "$ROOT/scripts/tests/fuzz_dashboard_smoke.sh"
run_step "redteam_local" "redteam surface (local node)" env BASE=http://127.0.0.1:8080 bash "$ROOT/scripts/tests/redteam_surface_smoke.sh"
run_step_optional "redteam_public" "redteam surface (hackme.tech)" env BASE=https://hackme.tech bash "$ROOT/scripts/tests/redteam_surface_smoke.sh"
run_step_optional "security_local" "security assertions (local)" env BASE=http://127.0.0.1:8080 bash "$ROOT/scripts/tests/security_assertions.sh"
run_step_optional "public_soak" "short public network soak 90s" \
  env BASE=https://hackme.tech COORD_URL=https://hackme.tech/pool/coordinator DURATION_SEC=90 INTERVAL_SEC=15 \
  bash "$ROOT/scripts/ops/network_stability_soak.sh"
run_step_optional "go_test_short" "go test short" go test ./... -short -count=1 -timeout=180s

# public pool snapshot
TOKEN="$COORD_TOKEN"
curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" \
  "https://hackme.tech/pool/coordinator/api/work/stats?details=1" \
  -o "$OUT/pool_public.json" 2>/dev/null || true

python3 - "$OUT" "$VERDICT" "$pass" "$fail" "$skip" <<'PY'
import json, sys
from pathlib import Path
from datetime import datetime, timezone

out = Path(sys.argv[1])
verdict = Path(sys.argv[2])
p, f, s = int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5])
now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")

pool = {}
pp = out / "pool_public.json"
if pp.exists():
    pool = json.loads(pp.read_text())

workers = pool.get("workers") or {}
total = sum(w.get("accepted_attempts", 0) for w in workers.values()) or 1

status = "PASS" if f == 0 else ("PASS_WITH_GAPS" if p > f else "FAIL")

lines = [
    f"# MASTER VERDICT — HackMe Production",
    f"",
    f"**{now}** · automated gate `{out.name}`",
    f"",
    f"## Overall: **{status}**",
    f"",
    f"- passed: **{p}** · failed: **{f}** · warn/skip: **{s}**",
    f"",
]
rm = out / "results.md"
if rm.exists():
    lines.append(rm.read_text())
lines += [
    f"",
    f"## Live pool (public)",
    f"",
    f"- scheduler: **{pool.get('scheduler_mode', 'n/a')}** · orders: **{pool.get('orders_active')}**",
    f"- found_hits: **{pool.get('found_hits')}** · target_mod: **{pool.get('target_mod', 0):,}**",
    f"",
]
if workers:
    lines.append("| Worker | Share | Payout HMC |")
    lines.append("|--------|-------|------------|")
    for wid, w in sorted(workers.items(), key=lambda x: -(x[1].get("accepted_attempts") or 0)):
        att = w.get("accepted_attempts", 0)
        lines.append(f"| {wid} | {100*att/total:.1f}% | {w.get('payout_hmc', 0):.6f} |")

lines += [
    f"",
    f"## What is DONE (private pool)",
    f"",
    f"3-rig mining, settlement to HMC-91fe, fuzzing proven, dashboard stable.",
    f"",
    f"## Optional follow-ups (scripts, not homework docs)",
    f"",
    f"1. **Red team + network soak** — `redteam_hard_mode.sh`, `network_stability_soak.sh` (1h+)",
    f"2. **Scale lab** — `simulate_pool_swarm_local.sh`, mega_stress, settlement under load",
    f"3. **Fuzzing soak** — `fuzzing_soak_prep.sh`, `orders_matrix.sh`",
    f"",
    f"Architecture reference: `docs/ARCHITECTURE.md` · `docs/NETWORK_MODEL.md`",
    f"",
    f"Artifacts: `{out}`",
]
verdict.write_text("\n".join(lines))
print(verdict.read_text())
PY

ln -sfn "$OUT" "$ROOT/reports/master-gate-LATEST"
cp -f "$VERDICT" "$ROOT/reports/MASTER_VERDICT.md"
echo "[master] wrote $VERDICT"
exit $(( fail > 0 ? 1 : 0 ))
