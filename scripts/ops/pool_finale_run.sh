#!/usr/bin/env bash
# One-shot pool finale: payout map, settlement, phasing probe, health snapshot.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
COORD_PUBLIC="${COORD_PUBLIC:-https://hackme.tech/pool/coordinator}"
REPORT_DIR="${REPORT_DIR:-$ROOT_DIR/reports/finale-$(date +%Y%m%dT%H%M%S)}"
MSK_SSH="${MSK_SSH:-root@82.146.53.7}"

mkdir -p "$REPORT_DIR"
log() { echo "[finale] $*" | tee -a "$REPORT_DIR/finale.log"; }

log "report dir: $REPORT_DIR"

# --- VPS canonical: deploy settle script + payout map ---
log "rsync settle_worker_payouts.sh"
rsync -avz "$ROOT_DIR/scripts/ops/settle_worker_payouts.sh" \
  "$NODE_SSH:$NODE_DEPLOY_DIR/scripts/ops/"

PAYOUT_MAP="worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET}"
log "WORKER_PAYOUT_MAP=$PAYOUT_MAP"
ssh "$NODE_SSH" "grep -q '^WORKER_PAYOUT_MAP=' '$NODE_DEPLOY_DIR/.env.settlement' 2>/dev/null && \
  sed -i 's|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=${PAYOUT_MAP}|' '$NODE_DEPLOY_DIR/.env.settlement' || \
  echo 'WORKER_PAYOUT_MAP=${PAYOUT_MAP}' >>'$NODE_DEPLOY_DIR/.env.settlement'"

# orders probe (idempotent)
ssh "$NODE_SSH" "grep -q '^HACKME_COORDINATOR_ORDERS_URL=' '$NODE_DEPLOY_DIR/.env.coord' 2>/dev/null || \
  echo 'HACKME_COORDINATOR_ORDERS_URL=http://127.0.0.1:18080' >>'$NODE_DEPLOY_DIR/.env.coord'"
ssh "$NODE_SSH" "grep -q '^HACKME_COORDINATOR_ORDERS_PRIORITY=' '$NODE_DEPLOY_DIR/.env.coord' 2>/dev/null || \
  echo 'HACKME_COORDINATOR_ORDERS_PRIORITY=1' >>'$NODE_DEPLOY_DIR/.env.coord'"
ssh "$NODE_SSH" "systemctl restart hackme-coordinator 2>/dev/null || true"

log "force settlement PC+MSK -> $WALLET"
ssh "$NODE_SSH" "cd '$NODE_DEPLOY_DIR' && set -a && . ./.env.settlement && set +a && \
  FORCE_SETTLE_ALL=1 MIN_SETTLE_HMC=0.0001 DAILY_MIN_SETTLE_HMC=0.0001 \
  bash scripts/ops/settle_worker_payouts.sh" | tee -a "$REPORT_DIR/settlement.log" || true

log "phasing order small if treasury funded"
ssh "$NODE_SSH" "ADMIN=\$(grep '^HACKME_ADMIN_TOKEN=' '$NODE_DEPLOY_DIR/.env.vps' | cut -d= -f2)
TS=\$(date +%s)
curl -sS -w '\\nHTTP %{http_code}\\n' -X POST http://127.0.0.1:18080/api/tasks \\
  -H 'Content-Type: application/json' -H \"X-Hackme-Admin-Token: \$ADMIN\" \\
  -d \"{\\\"id\\\":\\\"finale-phasing-\$TS\\\",\\\"kind\\\":\\\"synthetic_poh_v1\\\",\\\"difficulty_score\\\":5,\\\"reward_hmc\\\":0.012,\\\"target_solves\\\":2,\\\"payer_ref\\\":\\\"finale:auto\\\"}\"
sleep 4
curl -fsS http://127.0.0.1:18081/api/work/stats | jq '{orders_active,scheduler_mode,target_mod}' || true
" | tee -a "$REPORT_DIR/phasing_post.txt"

ssh "$NODE_SSH" "curl -fsS http://127.0.0.1:18081/api/work/stats?details=1" >"$REPORT_DIR/vps_coordinator_stats.json" 2>/dev/null || true
ssh "$NODE_SSH" "systemctl is-active hackme-node hackme-coordinator hackme-workerpoh" >"$REPORT_DIR/vps_services.txt" 2>/dev/null || true

# --- MSK VPS ---
if ssh -o BatchMode=yes -o ConnectTimeout=8 "$MSK_SSH" true 2>/dev/null; then
  ssh "$MSK_SSH" "systemctl is-active hackme-worker; tail -2 /opt/hackme-worker/logs/workerpoh.log" \
    >"$REPORT_DIR/msk_status.txt" 2>/dev/null || true
else
  log "MSK SSH skipped (no key/passwordless)"
fi

# --- Desktop ---
DESK_ENV="$ROOT_DIR/.env.desktop"
if [[ -f "$DESK_ENV" ]]; then
  if ! grep -q '^WORKER_PAYOUT_MAP=.*worker-vps-msk-01' "$DESK_ENV" 2>/dev/null; then
    sed -i "s|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=worker-kapa-pc=${WALLET},worker-vps-msk-01=${WALLET}|" "$DESK_ENV" || true
  fi
fi

pgrep -f 'workerpoh.*worker-kapa-pc' >/dev/null || {
  log "starting desktop worker"
  ADMIN_TOKEN="$(grep '^HACKME_ADMIN_TOKEN=' "$DESK_ENV" | cut -d= -f2-)" \
    bash "$ROOT_DIR/scripts/ops/worker_mine_start.sh" >>"$REPORT_DIR/desktop_worker.log" 2>&1 || true
}

TOKEN="$(tr -d '\r\n' <"$ROOT_DIR/.secrets/hackme_coordinator_admin_token")"
curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" "$COORD_PUBLIC/api/work/stats?details=1" \
  -o "$REPORT_DIR/public_coordinator_stats.json" 2>/dev/null || true

DESK_ADMIN="$(grep '^HACKME_ADMIN_TOKEN=' "$DESK_ENV" | cut -d= -f2-)"
curl -fsS "http://127.0.0.1:8080/api/worker/status" -H "X-Admin-Token: $DESK_ADMIN" \
  -o "$REPORT_DIR/desktop_worker.json" 2>/dev/null || true
curl -fsS "http://127.0.0.1:8080/api/wallet" -H "X-Admin-Token: $DESK_ADMIN" \
  -o "$REPORT_DIR/desktop_wallet.json" 2>/dev/null || true

# --- Verdict markdown ---
python3 - "$REPORT_DIR" "$WALLET" <<'PY'
import json, sys
from pathlib import Path
from datetime import datetime, timezone

report = Path(sys.argv[1])
wallet = sys.argv[2]
now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")

def load(name):
    p = report / name
    return json.loads(p.read_text()) if p.exists() else {}

pub = load("public_coordinator_stats.json")
desk_w = load("desktop_worker.json")
desk_bal = load("desktop_wallet.json")
workers = pub.get("workers") or {}
total = sum(w.get("accepted_attempts", 0) for w in workers.values()) or 1

lines = [
    f"# HackMe Pool — FINALE VERDICT",
    f"",
    f"**Generated:** {now}",
    f"**Primary wallet:** `{wallet}`",
    f"",
    f"## Status",
    f"",
    f"| Check | Result |",
    f"|-------|--------|",
    f"| Public coordinator | {'OK' if pub else 'FAIL'} |",
    f"| scheduler_mode | {pub.get('scheduler_mode', 'n/a')} |",
    f"| orders_active | {pub.get('orders_active')} |",
    f"| found_hits (pool) | {pub.get('found_hits', 'n/a')} |",
    f"| target_mod | {pub.get('target_mod', 'n/a')} |",
    f"| Desktop worker | {desk_w.get('running')} @ {desk_w.get('measured_hashrate_gh_s', 0):.1f} GH/s |",
    f"| Wallet balance (display) | {desk_bal.get('balance_hmc', 'n/a')} HMC |",
    f"",
    f"## Workers",
    f"",
]
for wid, w in sorted(workers.items(), key=lambda x: -(x[1].get("accepted_attempts") or 0)):
    att = w.get("accepted_attempts", 0)
    lines.append(
        f"- **{wid}**: {100*att/total:.1f}% | payout {w.get('payout_hmc',0):.6f} HMC | "
        f"hits {w.get('accepted_hits',0)} | addr {w.get('payout_address','—')}"
    )

lines += [
    f"",
    f"## Payout routing",
    f"- `WORKER_PAYOUT_MAP`: worker-kapa-pc + worker-vps-msk-01 → `{wallet}`",
    f"- settle_worker_payouts.sh: **map overrides** hybrid signed address",
    f"",
    f"## Artifacts",
    f"- `{report}`",
    f"",
    f"## Verdict",
]
all_three = all(k in workers for k in ("vps-canary-01", "worker-kapa-pc", "worker-vps-msk-01"))
if all_three and desk_w.get("running"):
    lines.append("**PASS — Multi-rig pool mining operational.** Settlement + phasing path configured.")
else:
    lines.append("**PARTIAL — Review finale.log and service units.**")

(report / "FINALE_VERDICT.md").write_text("\n".join(lines) + "\n")
print((report / "FINALE_VERDICT.md").read_text())
PY

log "done — see $REPORT_DIR/FINALE_VERDICT.md"
ln -sfn "$REPORT_DIR" "$ROOT_DIR/reports/finale-LATEST"
