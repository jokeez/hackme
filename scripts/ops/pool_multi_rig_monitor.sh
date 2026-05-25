#!/usr/bin/env bash
# Sample pool worker share + coordinator difficulty while orders/fuzzing run.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_TOKEN_FILE="${COORD_TOKEN_FILE:-$ROOT_DIR/.secrets/hackme_coordinator_admin_token}"
VPS_SSH="${VPS_SSH:-hackme-vps}"
SAMPLES="${SAMPLES:-12}"
INTERVAL_SEC="${INTERVAL_SEC:-15}"
OUT="${OUT:-$ROOT_DIR/reports/pool-multi-rig-$(date +%Y%m%dT%H%M%S)}"

mkdir -p "$OUT"
TOKEN="$(tr -d '\r\n' <"$COORD_TOKEN_FILE")"

log() { echo "[monitor] $*" | tee -a "$OUT/monitor.log"; }

sample_once() {
  local ts
  ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" \
    "$COORD_URL/api/work/stats?details=1" >"$OUT/stats_${ts}.json" 2>/dev/null || return 1
  if ssh -o BatchMode=yes -o ConnectTimeout=6 "$VPS_SSH" true 2>/dev/null; then
    ssh -o BatchMode=yes "$VPS_SSH" \
      'ADMIN=$(grep "^HACKME_ADMIN_TOKEN=" /opt/hackme/.env.vps | cut -d= -f2); curl -fsS http://127.0.0.1:18080/api/metrics -H "X-Hackme-Admin-Token: $ADMIN"' \
      >"$OUT/metrics_${ts}.json" 2>/dev/null || true
  fi
  python3 - "$OUT/stats_${ts}.json" "$OUT/metrics_${ts}.json" 2>>"$OUT/monitor.log" <<'PY' || true
import json, sys
from pathlib import Path

stats_path = Path(sys.argv[1])
metrics_path = Path(sys.argv[2]) if len(sys.argv) > 2 else None
j = json.loads(stats_path.read_text())
workers = j.get("workers") or {}
total = sum(int(w.get("accepted_attempts") or 0) for w in workers.values()) or 1
lines = [
    f"ts={stats_path.stem.replace('stats_', '')}",
    f"orders_active={j.get('orders_active')} scheduler={j.get('scheduler_mode')} target_mod={j.get('target_mod')}",
]
for wid, w in sorted(workers.items(), key=lambda x: -(x[1].get("accepted_attempts") or 0)):
    att = int(w.get("accepted_attempts") or 0)
    share = 100.0 * att / total
    lines.append(f"  {wid}: share={share:.2f}% attempts={att} ranges={w.get('accepted_ranges')} payout={w.get('payout_hmc', 0):.6f}")
if metrics_path and metrics_path.exists():
    m = json.loads(metrics_path.read_text())
    lines.append(
        f"  chain_target_mod={m.get('mining_target_mod')} obs_block_sec={m.get('mining_observed_block_sec')} blocks_1h={m.get('mining_poh_blocks_last_1h')}"
    )
print("\n".join(lines))
PY
}

log "out=$OUT samples=$SAMPLES interval=${INTERVAL_SEC}s"
for ((i = 1; i <= SAMPLES; i++)); do
  log "sample $i/$SAMPLES"
  sample_once || log "WARN: sample failed"
  sleep "$INTERVAL_SEC"
done
log "done — see $OUT/monitor.log"
