#!/usr/bin/env bash
# One-shot mining baseline: all workers, balances, coordinator, processes.
# Usage: bash scripts/ops/mining_night_baseline.sh [RUN_ID]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
[[ -f "$DESKTOP_ENV" ]] && set -a && . "$DESKTOP_ENV" && set +a

RUN_ID="${1:-night_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_DIR="${OUT_DIR:-$ROOT/reports/overnight}"
OUT="$OUT_DIR/$RUN_ID"
mkdir -p "$OUT"
ln -sfn "$OUT" "$OUT_DIR/CURRENT" 2>/dev/null || true

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
CANON_BASE="${CANON_BASE:-https://hackme.tech}"
COORD_BASE="${COORD_BASE:-https://hackme.tech/pool/coordinator}"
ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-}"
COORD_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN:-$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
WORKERS="${WORKERS:-worker-kapa-pc,worker-vps-msk-01,vps-canary-01,worker-vps-62-01}"

hdr=()
[[ -n "$ADMIN_TOKEN" ]] && hdr=(-H "X-Hackme-Admin-Token: $ADMIN_TOKEN")
coord_hdr=()
[[ -n "$COORD_TOKEN" ]] && coord_hdr=(-H "X-Hackme-Admin-Token: $COORD_TOKEN")

ts_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ts_local="$(date -Is)"

fetch() {
  local url="$1" timeout="${2:-25}"
  shift 2 || true
  curl -fsS --max-time "$timeout" "$@" "$url" 2>/dev/null || echo '{}'
}

st_lite="$(fetch "$LOCAL_BASE/api/status?lite=1" 20 "${hdr[@]}")"
st_full="$(fetch "$LOCAL_BASE/api/status" 30 "${hdr[@]}")"
wallet="$(fetch "$LOCAL_BASE/api/wallet?fresh=1" 35 "${hdr[@]}")"
work="$(fetch "$LOCAL_BASE/api/work/stats?details=1" 30 "${hdr[@]}")"
worker="$(fetch "$LOCAL_BASE/api/worker/status" 15 "${hdr[@]}")"
metrics="$(fetch "$LOCAL_BASE/api/metrics" 20 "${hdr[@]}")"
coord="$(fetch "$COORD_BASE/api/work/stats?details=1" 30 "${coord_hdr[@]}")"
coord_pub="$(fetch "$CANON_BASE/api/status?lite=1" 20)"
canon_wallet="$(fetch "$CANON_BASE/api/address/$WALLET" 25)"

settle_local='{}'
[[ -f "${HACKME_WORKER_SETTLEMENT_STATE_FILE:-$ROOT/logs/desktop/data/worker_settlement_state.json}" ]] && \
  settle_local="$(cat "${HACKME_WORKER_SETTLEMENT_STATE_FILE:-$ROOT/logs/desktop/data/worker_settlement_state.json}")"

settle_vps='{}'
if ssh -o BatchMode=yes -o ConnectTimeout=8 "$NODE_SSH" true 2>/dev/null; then
  settle_vps="$(ssh -o BatchMode=yes -o ConnectTimeout=12 "$NODE_SSH" \
    'cat /opt/hackme/data/worker_settlement_state.json 2>/dev/null || echo {}' 2>/dev/null || echo '{}')"
fi

pgrep -af 'hackme-node|workerpoh' >"$OUT/processes.txt" 2>/dev/null || true
ls -t "$ROOT"/logs/workerpoh-worker-kapa-pc-*.log 2>/dev/null | head -1 | xargs -r tail -n 8 >"$OUT/worker_kapa_tail.log" 2>/dev/null || true

jq -nc \
  --arg run_id "$RUN_ID" \
  --arg ts_utc "$ts_utc" \
  --arg ts_local "$ts_local" \
  --arg wallet "$WALLET" \
  --arg workers_csv "$WORKERS" \
  --arg git_commit "$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo nogit)" \
  --argjson st_lite "$st_lite" \
  --argjson st_full "$st_full" \
  --argjson wallet_api "$wallet" \
  --argjson work "$work" \
  --argjson worker "$worker" \
  --argjson metrics "$metrics" \
  --argjson coord "$coord" \
  --argjson coord_pub "$coord_pub" \
  --argjson canon_wallet "$canon_wallet" \
  --argjson settle_local "$settle_local" \
  --argjson settle_vps "$settle_vps" \
  '{
    run_id: $run_id,
    ts_utc: $ts_utc,
    ts_local: $ts_local,
    git_commit: $git_commit,
    wallet: $wallet,
    workers_expected: ($workers_csv | split(",")),
    local: {status_lite: $st_lite, status: $st_full, wallet: $wallet_api, work: $work, worker: $worker, metrics: $metrics, settlement: $settle_local},
    coordinator: {work: $coord, public_status: $coord_pub},
    canonical: {wallet: $canon_wallet},
    vps_settlement: $settle_vps
  }' >"$OUT/baseline_meta.json"
# Keep monitor snapshot format intact if overnight run already started.
if [[ ! -f "$OUT/snapshots.jsonl" ]] || [[ ! -s "$OUT/snapshots.jsonl" ]]; then
  cp -f "$OUT/baseline_meta.json" "$OUT/baseline.json"
else
  echo "[baseline] keeping existing $OUT/baseline.json (monitor snapshots present)"
fi

python3 - "$OUT/baseline.json" "$OUT/README.md" <<'PY'
import json, sys
from pathlib import Path

b = json.loads(Path(sys.argv[1]).read_text())
out = Path(sys.argv[2])

def g(obj, *path, default="—"):
    cur = obj
    for p in path:
        if not isinstance(cur, dict):
            return default
        cur = cur.get(p)
    return default if cur is None else cur

w = g(b, "local", "work", default={}) or {}
workers = (w.get("workers") or {}) if isinstance(w, dict) else {}
lines = [
    f"# Mining night baseline — {b.get('ts_local', '')} UTC {b.get('ts_utc', '')}",
    "",
    f"- **run_id:** `{b.get('run_id')}`",
    f"- **git:** `{b.get('git_commit')}`",
    f"- **wallet:** `{b.get('wallet')}`",
    "",
    "## Balances",
    f"- **canonical on-chain:** {float(g(b,'canonical','wallet','balance_units',default=0) or 0)/1e8:.8f} HMC",
    f"- **local wallet API:** {g(b,'local','wallet','balance_hmc',default='—')} HMC ({g(b,'local','wallet','wallet_source',default='')})",
    f"- **unpaid worker accrual:** {g(b,'local','wallet','unpaid_worker_accrual_hmc',default='—')}",
    "",
    "## Chain / pool",
    f"- **local tip:** {g(b,'local','status_lite','tip_height')}",
    f"- **canonical tip:** {g(b,'local','status_lite','canonical_tip_height')}",
    f"- **pool TH/s:** {g(b,'local','status_lite','pool_global_hashrate_th_s')}",
    f"- **pool miners:** {g(b,'local','status_lite','pool_total_miners')}",
    "",
    "## Coordinator totals",
    f"- **total payout HMC:** {w.get('total_payout_hmc', '—')}",
    f"- **submitted ranges:** {w.get('submitted_items', '—')}",
    f"- **accepted attempts:** {w.get('accepted_attempts', '—')}",
    f"- **reward/M:** {w.get('reward_per_m', '—')}",
    "",
    "## Workers",
    "| worker | GH/s | ranges | attempts | payout HMC |",
    "|--------|------|--------|----------|------------|",
]
for wid in sorted(workers.keys()):
    ww = workers[wid] or {}
    gh = ww.get("hashrate_gh_s") or (ww.get("live") or {}).get("hashrate_gh_s") or 0
    lines.append(
        f"| {wid} | {float(gh):.4f} | {ww.get('accepted_ranges','—')} | {ww.get('accepted_attempts','—')} | {float(ww.get('payout_hmc') or 0):.6f} |"
    )
wk = g(b, "local", "worker", default={}) or {}
lines += [
    "",
    "## Difficulty / economics (now)",
    f"- **pool target_mod (M):** {w.get('target_mod', '—'):,}" if isinstance(w.get("target_mod"), (int, float)) else "- **pool target_mod (M):** —",
    f"- **reward/M:** {w.get('reward_per_m', '—')}",
    f"- **found bonus:** {w.get('found_bonus_hmc', '—')} HMC",
    f"- **auto-retarget:** {w.get('pool_retarget_enabled', '—')}",
    f"- **scheduler:** {w.get('scheduler_mode', '—')}",
    f"- **target_mod updated unix:** {w.get('target_mod_updated_unix', '—')}",
    "",
    "Overnight trace: `reports/overnight/CURRENT/difficulty.jsonl` + `DIFFICULTY.md`",
    "",
    "## Desktop worker process",
    f"- **running:** {wk.get('running', '—')}",
    f"- **worker_id:** {wk.get('worker_id', '—')}",
    f"- **measured GH/s:** {wk.get('measured_hashrate_gh_s', '—')}",
    "",
    "Compare tomorrow: `jq .delta reports/overnight/CURRENT/summary.json`",
]
out.write_text("\n".join(lines) + "\n")
print(out)
PY

cp -f "$OUT/baseline.json" "$OUT_DIR/baseline_latest.json" 2>/dev/null || true
echo "[baseline] OK run_id=$RUN_ID"
echo "[baseline] json=$OUT/baseline.json"
echo "[baseline] readme=$OUT/README.md"
cat "$OUT/README.md"
