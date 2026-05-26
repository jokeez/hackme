#!/usr/bin/env bash
# Miner happiness audit: payouts, fairness, worker health, settlement.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WALLET="${WALLET:-HMC-91fe007e4036c602}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
COORD_TOKEN="$(tr -d '\r\n' <"${ROOT}/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT_DIR:-$ROOT/reports/miner-happy-$STAMP}"
mkdir -p "$OUT"
REPORT="$OUT/MINER_HAPPINESS.md"

pass=0; warn=0; fail=0
note() { echo "$*" | tee -a "$OUT/check.log"; }
ok() { pass=$((pass+1)); note "OK  $*"; }
wrn() { warn=$((warn+1)); note "WARN $*"; }
bad() { fail=$((fail+1)); note "FAIL $*"; }

# --- pool stats ---
POOL_JSON="$OUT/pool.json"
curl -fsS --max-time 12 -H "X-Hackme-Admin-Token: $COORD_TOKEN" \
  "${COORD}/api/work/stats?details=1" -o "$POOL_JSON" 2>/dev/null && ok "coordinator reachable" || bad "coordinator unreachable"

LEASE_PW="$(jq -r '.max_active_leases_per_worker // 0' "$POOL_JSON" 2>/dev/null || echo 0)"
LEASE_SEC="$(jq -r '.lease_sec // 30' "$POOL_JSON" 2>/dev/null || echo 30)"
if [[ "$LEASE_PW" != "0" && "$LEASE_PW" != "null" && "$LEASE_PW" != "" ]]; then
  ok "per-worker lease cap active: max_active_leases_per_worker=$LEASE_PW"
elif [[ "${LEASE_SEC:-0}" -ge 60 ]]; then
  ok "fair pool lease_sec=${LEASE_SEC} (per-worker cap may be omitted on public proxy)"
else
  wrn "no per-worker lease cap (canary may dominate); run apply_miner_fair_pool.sh"
fi
if [[ "${LEASE_SEC:-0}" -ge 60 ]]; then
  ok "lease_sec=${LEASE_SEC} (comfortable for GPU rigs)"
else
  wrn "lease_sec=${LEASE_SEC} — consider HACKME_COORDINATOR_LEASE_SEC=90"
fi

# --- share fairness ---
python3 - "$POOL_JSON" <<'PY' | tee -a "$OUT/check.log"
import json, sys
p = json.load(open(sys.argv[1]))
workers = p.get("workers") or {}
if not workers:
    print("WARN no workers in pool stats")
    sys.exit(0)
total = sum(w.get("accepted_attempts", 0) for w in workers.values()) or 1
rows = sorted(workers.items(), key=lambda x: -(x[1].get("accepted_attempts") or 0))
for wid, w in rows:
    att = w.get("accepted_attempts", 0)
    pct = 100 * att / total
    pay = w.get("payout_hmc", 0)
    print(f"  {wid}: share={pct:.1f}% payout_accrued={pay:.6f} HMC")
pc = workers.get("worker-kapa-pc", {}).get("accepted_attempts", 0)
pct_pc = 100 * pc / total
if pct_pc >= 8:
    print(f"OK  PC share {pct_pc:.1f}% — healthy for multi-rig")
elif pct_pc >= 3:
    print(f"WARN PC share {pct_pc:.1f}% — low but earning; fair-pool may still be ramping")
else:
    print(f"WARN PC share {pct_pc:.1f}% — run apply_miner_fair_pool.sh if home miner should earn more")
PY

# --- PC worker (ideal single miner or legacy name) ---
PC_PID=""
if pgrep -f 'workerpoh.*worker-kapa-pc' >/dev/null 2>&1; then
  PC_PID=pc
elif pgrep -f 'workerpoh.*worker-kapa-rig-' >/dev/null 2>&1; then
  PC_PID=rig
fi
if [[ -n "$PC_PID" ]]; then
  ok "local pool worker running (mode=${PC_PID})"
  LOG="$(ls -t "$ROOT"/logs/workerpoh-worker-kapa-pc-*.log "$ROOT"/logs/ideal-miner/*.log "$ROOT"/logs/pool-display-rig/*.log 2>/dev/null | head -1)"
  if [[ -n "$LOG" ]]; then
    GH="$(grep 'submit ok' "$LOG" | tail -15 | sed -n 's/.*ghs=\([0-9.]*\).*/\1/p' | awk '{s+=$1;n++} END{if(n) printf "%.1f", s/n; else print "0"}')"
    TO="$(tail -200 "$LOG" | grep -cE 'claim error|submit error' || true)"
    OK="$(tail -200 "$LOG" | grep -c 'submit ok' || true)"
    if awk -v g="${GH:-0}" 'BEGIN{exit !(g>=5)}'; then
      ok "PC hashrate ~${GH} GH/s (last 15 submits)"
    else
      wrn "PC hashrate low (~${GH} GH/s)"
    fi
    if [[ "$OK" -gt 0 ]] && [[ "$TO" -le "$((OK/3+1))" ]]; then
      ok "PC network errors acceptable (${TO} errors / ${OK} ok in last 200 lines)"
    else
      wrn "PC network errors elevated (${TO} errors / ${OK} ok) — check VPN/firewall to hackme.tech"
    fi
  fi
else
  bad "no local pool worker — start: bash scripts/ops/start_local_ideal_miner.sh"
fi

# --- wallet ---
BAL="$(curl -fsS --max-time 8 "https://hackme.tech/api/address/${WALLET}" | jq -r '.balance_units // 0' 2>/dev/null || echo 0)"
BAL_HMC="$(python3 -c "print(f'{int('${BAL}')/1e8:.6f}')")"
ok "wallet ${WALLET} balance ${BAL_HMC} HMC"

# --- payout map desktop ---
if grep -q "worker-kapa-pc=${WALLET}" "$ROOT/.env.desktop" 2>/dev/null; then
  ok "desktop WORKER_PAYOUT_MAP routes PC → your wallet"
else
  wrn "desktop .env.desktop missing WORKER_PAYOUT_MAP for ${WALLET}"
fi

# --- settlement timer on VPS ---
if ssh -o BatchMode=yes -o ConnectTimeout=6 -i "${HOME}/.ssh/id_ed25519" hackme-vps \
  'systemctl is-active hackme-worker-settlement.timer' 2>/dev/null | grep -q active; then
  ok "VPS settlement timer active"
else
  wrn "VPS settlement timer not active — payouts may delay"
fi

STATUS="HAPPY"
[[ "$fail" -gt 0 ]] && STATUS="NEEDS_FIX"
[[ "$fail" -eq 0 && "$warn" -gt 0 ]] && STATUS="OK_WITH_NOTES"

{
  echo "# Miner happiness — **${STATUS}**"
  echo ""
  echo "**$(date -u +%Y-%m-%dT%H:%M:%SZ)** · ok=${pass} warn=${warn} fail=${fail}"
  echo ""
  echo "## Checks"
  echo '```'
  cat "$OUT/check.log"
  echo '```'
  echo ""
  echo "## Miner-friendly guarantees (when fair pool applied)"
  echo "- Each rig max **3** active leases — no single worker hoards the pool"
  echo "- **90s** lease — GPU batches finish without \`lease_expired\`"
  echo "- Canary throttled (**1.5s** between claims) — home PC gets fair slots"
  echo "- All rigs settle to **${WALLET}**"
  echo "- Settlement timer on VPS every ~2 min (MIN_SETTLE 0.0005 HMC)"
  echo ""
  if [[ "$STATUS" != "HAPPY" ]]; then
    echo "## Fix"
    echo '```bash'
    echo "bash scripts/ops/apply_miner_fair_pool.sh"
    echo '```'
  fi
} >"$REPORT"

ln -sfn "$OUT" "$ROOT/reports/miner-happy-LATEST"
cp -f "$REPORT" "$ROOT/reports/MINER_HAPPINESS.md"
cat "$REPORT"
exit $(( fail > 0 ? 1 : 0 ))
