#!/usr/bin/env bash
# Economics, halving, supply invariants, pool fairness, wallet reconcile — confidence battery.
#
#   bash scripts/tests/economics_confidence_gate.sh
#   PROD_BASE=https://hackme.tech LOCAL_BASE=http://127.0.0.1:8080 bash scripts/tests/economics_confidence_gate.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/economics-confidence-$STAMP}"
mkdir -p "$OUT"

PROD_BASE="${PROD_BASE:-https://hackme.tech}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
PROD_BASE="${PROD_BASE%/}"
LOCAL_BASE="${LOCAL_BASE%/}"
COORD="${COORD:-${PROD_BASE}/pool/coordinator}"

ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"
if [[ -z "$ADMIN_TOKEN" ]] && [[ -f "$ROOT/.env.desktop" ]]; then
  ADMIN_TOKEN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$ROOT/.env.desktop" 2>/dev/null | cut -d= -f2- || true)"
fi
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN_TOKEN}"
export ADMIN_TOKEN="$HACKME_ADMIN_TOKEN"

pass=0
fail=0
skip=0
VERDICT="$OUT/VERDICT.md"

log() { echo "[econ-gate] $*" | tee -a "$OUT/run.log"; }

run_step() {
  local id="$1" desc="$2"
  shift 2
  log "=== $id: $desc ==="
  if "$@" >"$OUT/${id}.log" 2>&1; then
    log "PASS $id"
    echo "| $id | PASS | $desc |" >>"$OUT/results.md"
    pass=$((pass + 1))
  else
    log "FAIL $id (see $OUT/${id}.log)"
    echo "| $id | **FAIL** | $desc |" >>"$OUT/results.md"
    fail=$((fail + 1))
  fi
}

run_optional() {
  local id="$1" desc="$2"
  shift 2
  log "=== $id (optional): $desc ==="
  if "$@" >"$OUT/${id}.log" 2>&1; then
    log "PASS $id"
    echo "| $id | PASS | $desc |" >>"$OUT/results.md"
    pass=$((pass + 1))
  else
    log "WARN/SKIP $id"
    echo "| $id | WARN | $desc |" >>"$OUT/results.md"
    skip=$((skip + 1))
  fi
}

: >"$OUT/results.md"
echo "# Economics confidence gate — $STAMP" >"$VERDICT"
echo "" >>"$VERDICT"
echo "| Step | Result | Description |" >>"$OUT/results.md"
echo "|------|--------|-------------|" >>"$OUT/results.md"

# --- Unit: halving, policy lock, supply identity ---
run_step "go_chain_economics" "chain halving + locked policy + econ invariants" \
  go test -count=1 -timeout=120s ./internal/chain/ \
    -run 'TestLockedPolicy|TestValidateLockedPolicy|TestBaseRewardForBlockIndex|TestNextHalvingBlock|TestExpectedEmptyMining|TestNetworkFee|TestDifficultyFairness|TestEconomics|TestEconomic|TestAppendPoHBlockRejectsEconomic'

run_step "go_store_economics_meta" "SQLite economics units migration" \
  go test -count=1 -timeout=60s ./internal/store/ -run TestOpenMigratesEconomics

run_step "go_hms_economics" "HMS ledger + kernel rates + market payment" \
  go test -count=1 -timeout=120s ./internal/chain/ \
    -run 'TestHMS|TestPayHMS'

# --- Live prod: consensus economics on canonical chain ---
run_step "exchange_listing_smoke" "prod status economics identity + dev fee" \
  env BASE="$PROD_BASE" CURL_MAX=45 bash "$ROOT/scripts/ops/exchange_listing_smoke.sh"

run_optional "pool_fairness_prod" "prod pool reward/M vs payout model (3 samples)" \
  env COORD_URL="$COORD" SAMPLES=3 INTERVAL_SEC=10 bash "$ROOT/scripts/ops/pool_fairness_audit.sh"

# --- Local node (if up) ---
if curl -fsS --max-time 5 "${LOCAL_BASE}/api/status?lite=1" >/dev/null 2>&1; then
  run_step "security_local_econ" "local security assertions (economics invariants)" \
    env BASE="$LOCAL_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash "$ROOT/scripts/tests/security_assertions.sh"
  run_optional "penny_reconcile" "wallet vs pool penny reconcile" \
    env OUT="$OUT/penny" LOCAL_BASE="$LOCAL_BASE" PROD_BASE="$PROD_BASE" COORD="$COORD" \
    bash "$ROOT/scripts/ops/penny_reconcile.sh"
else
  log "SKIP local jobs — ${LOCAL_BASE} down"
  echo "| security_local_econ | SKIP | local node down |" >>"$OUT/results.md"
  echo "| penny_reconcile | SKIP | local node down |" >>"$OUT/results.md"
  skip=$((skip + 2))
fi

# --- Halving + metrics snapshot (prod + local) ---
_halving_probe() {
  python3 - "$OUT" "$PROD_BASE" "$LOCAL_BASE" <<'PY'
import json, sys, urllib.request
from pathlib import Path

out = Path(sys.argv[1])
prod, local = sys.argv[2], sys.argv[3]

def get(url, timeout=25):
    import subprocess
    try:
        out = subprocess.check_output(
            ["curl", "-fsS", "--max-time", str(timeout), url],
            stderr=subprocess.DEVNULL,
        )
        return json.loads(out)
    except Exception as e:
        return {"_error": str(e)}

def econ_block(label, base):
    st = get(f"{base}/api/status?lite=1", timeout=12) or get(f"{base}/api/status", timeout=25)
    m = get(f"{base}/api/metrics")
    ec = st.get("economics") if isinstance(st.get("economics"), dict) else {}
    if not ec and isinstance(st, dict):
        ec = {k: st.get(k) for k in (
            "total_minted_hmc", "total_burned_hmc", "circulating_hmc",
            "max_supply_hmc", "mint_remaining_hmc", "policy_hash", "dev_fee_address",
        ) if st.get(k) is not None}
        if ec:
            st = dict(st)
            st["economics"] = ec
    lines = [f"## {label} ({base})", ""]
    if st.get("_error"):
        lines.append(f"- status: ERROR {st['_error']}")
        return lines, False
    ec = st.get("economics") or {}
    tip = int(st.get("tip_height") or 0)
    lines += [
        f"- tip_height: {tip}",
        f"- policy_hash: {str(ec.get('policy_hash',''))[:16]}…",
        f"- minted_hmc: {ec.get('total_minted_hmc')}",
        f"- burned_hmc: {ec.get('total_burned_hmc')}",
        f"- circulating_hmc: {ec.get('circulating_hmc')}",
    ]
    mint = float(ec.get("total_minted_hmc") or 0)
    burn = float(ec.get("total_burned_hmc") or 0)
    circ = float(ec.get("circulating_hmc") or 0)
    ok_id = mint > 0 and abs((mint - burn) - circ) < 1e-2
    lines.append(f"- circulating identity: {'OK' if ok_id else 'FAIL'}")
    if m and not m.get("_error"):
        nh = int(m.get("econ_next_halving_block") or 0)
        iv = int(m.get("econ_reward_halving_interval_blocks") or 0)
        lines.append(f"- econ_next_halving_block: {nh}")
        lines.append(f"- halving_interval_blocks: {iv}")
        if nh > tip and iv > 0:
            lines.append(f"- blocks_until_halving: {nh - tip}")
    ok = ok_id and tip > 0 and bool(ec.get("policy_hash"))
    return lines, ok

report = ["# Halving / economics live probe", ""]
ok_prod = False
for label, base in [("Production", prod), ("Local", local)]:
    lines, ok = econ_block(label, base)
    report.extend(lines)
    report.append("")
    if label == "Production":
        ok_prod = ok
(out / "halving_probe.md").write_text("\n".join(report))
print("\n".join(report))
sys.exit(0 if ok_prod else 1)
PY
}
run_step "halving_live_probe" "prod/local economics + halving metrics fields" _halving_probe

# --- Order economics audit (static, no chain) ---
if [[ -f "$ROOT/scripts/ops/order_economics_audit.py" ]]; then
  run_optional "order_economics_audit" "order fee / burn model static audit" \
    python3 "$ROOT/scripts/ops/order_economics_audit.py"
fi

# --- Verdict ---
{
  echo ""
  echo "## Summary"
  echo ""
  echo "- **PASS:** $pass"
  echo "- **FAIL:** $fail"
  echo "- **WARN/SKIP:** $skip"
  echo ""
  if [[ "$fail" -eq 0 ]]; then
    echo "## Verdict: **GO**"
    echo ""
    echo "Halving math (unit), locked policy hash, supply identity, and prod economics API look consistent."
  else
    echo "## Verdict: **NO-GO**"
    echo ""
    echo "Fix failing steps in \`$OUT/*.log\` before exchange / public economics claims."
  fi
} | tee -a "$VERDICT"

cat "$OUT/results.md" >>"$VERDICT"
echo "[econ-gate] report: $VERDICT"
[[ "$fail" -eq 0 ]]
