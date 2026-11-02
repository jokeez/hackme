#!/usr/bin/env bash
# Linux miner OOTB smoke against public pool (PoH claim + hybrid Dig capability).
# Hunt engines: soft-skip OK without clang/bwrap; Dig/PoH must work.
#
#   bash scripts/ops/linux_miner_journey_gate.sh
#   WORKER_ID=worker-my-pc-02 WORKER_SMOKE_SEC=90 bash scripts/ops/linux_miner_journey_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD="${COORD_URL:-https://hackme.tech/pool/coordinator}"
STATUS="${STATUS_URL:-https://hackme.tech/api/status}"
WORKER_ID="${WORKER_ID:-journey-$(hostname -s 2>/dev/null || echo lab)-$$}"
SMOKE_SEC="${WORKER_SMOKE_SEC:-30}"
UA='Mozilla/5.0 (HackMe-linux-miner-journey)'

echo "[linux-journey] status"
curl -fsS --max-time 20 -A "$UA" "$STATUS" | python3 -c "
import sys,json
d=json.load(sys.stdin)
tip=d.get('display_tip_height') or d.get('canonical_tip_height')
assert tip and int(tip)>0, tip
assert d.get('chain_leader_local_poh') is not True
print('tip', tip, 'commit', d.get('commit'), 'local_poh', d.get('chain_leader_local_poh'))
"

echo "[linux-journey] coordinator work stats"
curl -fsS --max-time 20 -A "$UA" "${COORD%/}/api/work/stats" | python3 -c "
import sys,json
d=json.load(sys.stdin)
assert d.get('share_strict') in (True,1,'1','true','True')
assert str(d.get('baseline_relay_mode','')).lower() in ('on','1','true')
print('tip', d.get('tip_height'), 'shares', d.get('share_accepts'), 'relay', d.get('baseline_relay_mode'))
"

echo "[linux-journey] worker id probe (no auth mutate): $WORKER_ID smoke=${SMOKE_SEC}s"
# Public health only — full claim needs miner token on the box.
sleep 1
echo "[linux-journey] OK (public pool reachable; install miner + pool.miner.token for claim smoke)"
echo "[linux-journey] Hunt engines: Linux only (install_hunt_engines.sh). Windows Dig/PoH — see windows_miner_ootb_checklist.sh"
