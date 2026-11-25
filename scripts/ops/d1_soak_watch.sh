#!/usr/bin/env bash
# RFC D1 live soak observer — tip / relay reject / SHARE_STRICT / settlement float+unpaid.
#
#   bash scripts/ops/d1_soak_watch.sh
#   STRICT=1 bash scripts/ops/d1_soak_watch.sh   # exit 2 on soft WARN thresholds
#
# Appends one JSON line to logs/d1-soak/watch.jsonl (override with OUT_JSONL=).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
STATUS_URL="${STATUS_URL:-https://hackme.tech/pool/api/status}"
SETTLE_URL="${SETTLE_URL:-https://hackme.tech/pool/api/worker/settlement}"
CHAIN_API="${CHAIN_API:-https://hackme.tech/pool/api}"
# Hub primary / D1 coinbase destination (checkpoint signer / node_address).
SETTLEMENT_PRIMARY="${SETTLEMENT_PRIMARY:-HMC-dfc143732a1fc3bc}"
# Legacy order/treasury float (still monitored).
SETTLEMENT_FLOAT_LEGACY="${SETTLEMENT_FLOAT_LEGACY:-HMC-381c0c5e2cfcc714}"
OUT_JSONL="${OUT_JSONL:-$ROOT/logs/d1-soak/watch.jsonl}"
MAX_REJECT_PCT="${MAX_REJECT_PCT:-5}"
# After coordinator restart counters reset — ignore reject_pct until enough samples.
MIN_RELAY_SAMPLES="${MIN_RELAY_SAMPLES:-40}"
MAX_FLEET_UNPAID_HMC="${MAX_FLEET_UNPAID_HMC:-200}"
REQUIRE_FLOAT_COVERS_UNPAID="${REQUIRE_FLOAT_COVERS_UNPAID:-1}"
STRICT="${STRICT:-0}"
UA='Mozilla/5.0 (HackMe-d1-soak-watch)'

mkdir -p "$(dirname "$OUT_JSONL")"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

curl -fsS --max-time 25 -A "$UA" "${COORD_URL%/}/api/work/stats" -o "$tmp/stats.json"
curl -fsS --max-time 20 -A "$UA" "$STATUS_URL" -o "$tmp/status.json"
curl -fsS --max-time 20 -A "$UA" "$SETTLE_URL" -o "$tmp/settle.json"
curl -fsS --max-time 15 -A "$UA" "${CHAIN_API%/}/address/${SETTLEMENT_PRIMARY}" -o "$tmp/prim.json" || echo '{}' >"$tmp/prim.json"
curl -fsS --max-time 15 -A "$UA" "${CHAIN_API%/}/address/${SETTLEMENT_FLOAT_LEGACY}" -o "$tmp/leg.json" || echo '{}' >"$tmp/leg.json"

# Disk, because 2026-11-25 happened: the store filled to 100%, the chain node entered a
# restart loop on "sqlite: disk I/O error", the public site answered 502 and the exchange
# could not verify its reserves. Every one of those was predictable hours earlier from one
# df line that nothing looked at. Measured locally — this watch runs on the hub.
DISK_PATH="${DISK_PATH:-/opt/hackme}"
MIN_DISK_FREE_GB="${MIN_DISK_FREE_GB:-4}"
WARN_DISK_FREE_GB="${WARN_DISK_FREE_GB:-12}"
disk_avail_gb="$(df -B1 --output=avail "$DISK_PATH" 2>/dev/null | tail -1 | awk '{printf "%.2f", $1/1073741824}')"
disk_pct="$(df --output=pcent "$DISK_PATH" 2>/dev/null | tail -1 | tr -dc '0-9')"
# The remedy is only worth computing when the alarm is in play: the collector walks
# ~300k objects, which is not something to do every few minutes for a healthy disk.
collectable_gb=""
if [[ -n "$disk_avail_gb" ]] && awk "BEGIN{exit !($disk_avail_gb < $WARN_DISK_FREE_GB)}"; then
  gc_bin="${CORPUS_GC_BIN:-/opt/hackme/bin/corpus-gc}"
  fuzz_db="${CORPUS_GC_DB:-/opt/hackme/data/coordinator_fuzz.db}"
  if [[ -x "$gc_bin" && -f "$fuzz_db" ]]; then
    collectable_gb="$("$gc_bin" -dir "${fuzz_db}.corpus-objects" -db "$fuzz_db" 2>/dev/null \
      | awk '/collectable/ {print $(NF-1)}' | tail -1)"
  fi
fi

python3 - "$tmp" "$OUT_JSONL" "$MAX_REJECT_PCT" "$MAX_FLEET_UNPAID_HMC" \
  "$REQUIRE_FLOAT_COVERS_UNPAID" "$STRICT" "$SETTLEMENT_PRIMARY" "$SETTLEMENT_FLOAT_LEGACY" \
  "$MIN_RELAY_SAMPLES" "$disk_avail_gb" "$disk_pct" "$MIN_DISK_FREE_GB" "$WARN_DISK_FREE_GB" \
  "$collectable_gb" "$DISK_PATH" <<'PY'
import json, sys, time
tmp, out_path, max_rej, max_unpaid, need_cover, strict, prim_addr, leg_addr, min_samples = sys.argv[1:10]
disk_avail_s, disk_pct_s, min_disk_s, warn_disk_s, collectable_s, disk_path = (sys.argv[10:16] + [""] * 6)[:6]


def _f(v):
    try:
        return float(str(v).strip())
    except Exception:
        return None


disk_avail = _f(disk_avail_s)
disk_pct = _f(disk_pct_s)
min_disk = _f(min_disk_s) or 4.0
warn_disk = _f(warn_disk_s) or 12.0
collectable = _f(collectable_s)
max_rej = float(max_rej)
max_unpaid = float(max_unpaid)
min_samples = int(float(min_samples))
need_cover = need_cover not in ("0", "false", "False", "")
strict = strict in ("1", "true", "True", "yes")

def load(name):
    with open(f"{tmp}/{name}") as f:
        return json.load(f)

stats, status, settle, prim, leg = map(load, ("stats.json", "status.json", "settle.json", "prim.json", "leg.json"))
ok = float(stats.get("baseline_relay_ok") or 0)
rej = float(stats.get("baseline_relay_reject") or 0)
tot = ok + rej
# Why the chain refused, not just how often. A bare rate cannot separate two miners
# racing the same height — which costs the miner nothing, the lease's shares stay
# payable — from a defect, so the alarm named a number nobody could act on.
def _reasons(key):
    v = stats.get(key)
    return {str(k): int(n) for k, n in v.items()} if isinstance(v, dict) else {}
reject_reasons = _reasons("baseline_relay_reject_reasons")
skip_reasons = _reasons("baseline_relay_skip_reasons")
top_reject = max(reject_reasons.items(), key=lambda kv: (kv[1], kv[0]))[0] if reject_reasons else ""
reject_pct = (100.0 * rej / tot) if tot else None
unpaid = float(settle.get("fleet_unpaid_hmc") or settle.get("total_unpaid_hmc") or 0)
prim_bal = float((prim or {}).get("balance_hmc") or 0)
leg_bal = float((leg or {}).get("balance_hmc") or 0)
# Settler pays from primary (node wallet). Legacy is observability-only — do not treat it as cover.
combined = prim_bal + leg_bal
# unpaid is only evidence when the view can see both sides of the ledger. Measured on
# 2026-11-24 against the URL this watch actually reads: total_accrued 3579.77,
# total_settled 0, accrual_source "none" — the coordinator omits the per-worker
# breakdown, so nothing attributes settlements and "unpaid" is lifetime accrual wearing
# a debt's clothes. The chain says otherwise: 2,162 payout transactions and 286 HMC in
# the preceding 24h, and worker_accrual's lifetime 4,511 HMC matches the coordinator's
# 4,513. So this alarm was HARD on every single run, which is how an operator learns to
# stop reading it. Both blind spots now degrade to a warning instead.
stats_stale = bool(settle.get("coordinator_stats_stale"))
settled_hmc = float(settle.get("total_settled_hmc") or settle.get("wallet_settled_hmc") or 0)
accrual_src = str(settle.get("accrual_source") or "").strip().lower()
unpaid_blind = accrual_src in ("", "none") or (settled_hmc <= 0 and unpaid > 0)
local_poh = status.get("chain_leader_local_poh")
tip = status.get("canonical_tip_height") or stats.get("tip_height")
row = {
    "ts": int(time.time()),
    "coordinator_stats_stale": stats_stale,
    "disk_path": disk_path or None,
    "disk_free_gb": disk_avail,
    "disk_used_pct": disk_pct,
    "corpus_collectable_gb": collectable,
    "settlement_settled_hmc": round(settled_hmc, 8),
    "settlement_accrual_source": accrual_src or None,
    "unpaid_is_measurable": not (stats_stale or unpaid_blind),
    "tip": tip,
    "local_poh": local_poh,
    "relay_mode": stats.get("baseline_relay_mode"),
    "relay_ok": int(ok),
    "relay_reject": int(rej),
    "reject_pct": None if reject_pct is None else round(reject_pct, 3),
    "baseline_relay_reject_reasons": reject_reasons,
    "baseline_relay_skip_reasons": skip_reasons,
    "share_strict": stats.get("share_strict"),
    "share_accepts": stats.get("share_accepts"),
    "pool_gh": stats.get("pool_hashrate_gh_s"),
    "fleet_unpaid_hmc": round(unpaid, 6),
    "settlement_primary": prim_addr,
    "settlement_primary_hmc": round(prim_bal, 8),
    "settlement_legacy_float": leg_addr,
    "settlement_legacy_hmc": round(leg_bal, 8),
    "settlement_combined_hmc": round(combined, 8),
    "float_covers_unpaid": combined + 1e-12 >= unpaid,
    "primary_covers_unpaid": prim_bal + 1e-12 >= unpaid,
}
hard, soft = [], []
if local_poh is True:
    hard.append("local_poh_enabled")
if str(stats.get("baseline_relay_mode") or "").lower() not in ("on", "1", "true"):
    hard.append("relay_not_on")
if reject_pct is not None and reject_pct > max_rej:
    if tot >= min_samples:
        hard.append(f"reject_pct>{max_rej}" + (f"({top_reject})" if top_reject else ""))
    else:
        soft.append(f"reject_pct>{max_rej}_warmup_n={int(tot)}" + (f"({top_reject})" if top_reject else ""))
if unpaid > max_unpaid:
    if stats_stale:
        soft.append(f"fleet_unpaid>{max_unpaid}_stats_stale")
    elif unpaid_blind:
        soft.append(f"fleet_unpaid>{max_unpaid}_no_settled_side")
    else:
        hard.append(f"fleet_unpaid>{max_unpaid}")
if stats.get("share_strict") is not True and str(stats.get("share_strict")).lower() not in ("1", "true"):
    soft.append("share_strict_off")

# A full disk takes the chain down, so this is HARD well before it is full: at
# MIN_DISK_FREE_GB there is still room to act. When the corpus collector can name the
# reclaimable amount, the alarm carries the remedy instead of only the symptom.
if disk_avail is None:
    soft.append("disk_unreadable")
elif disk_avail < min_disk:
    note = f"disk_free<{min_disk:g}GB({disk_avail:.1f}GB)"
    if collectable:
        note += f"_collectable={collectable:.1f}GB"
    hard.append(note)
elif disk_avail < warn_disk:
    note = f"disk_free<{warn_disk:g}GB({disk_avail:.1f}GB)"
    if collectable:
        note += f"_collectable={collectable:.1f}GB"
    soft.append(note)
# Prefer primary: settler cannot spend legacy. combined-only cover is a soft WARN.
if need_cover and not row["primary_covers_unpaid"]:
    soft.append("settler_payer_shortfall")
if need_cover and not row["float_covers_unpaid"]:
    soft.append("combined_float_below_unpaid")
row["hard_alerts"] = hard
row["soft_alerts"] = soft
row["ok"] = not hard and (not strict or not soft)
with open(out_path, "a", encoding="utf-8") as f:
    f.write(json.dumps(row, separators=(",", ":")) + "\n")
if disk_avail is not None:
    extra = f" collectable={collectable:.1f}GB" if collectable else ""
    print("[d1-soak] disk=%s free=%.1fGB used=%s%%%s" % (disk_path, disk_avail, int(disk_pct) if disk_pct else "?", extra))
print("[d1-soak] tip=%s local_poh=%s reject_pct=%s unpaid=%.4f primary=%.4f legacy=%.4f combined=%.4f primary_covers=%s" % (
    tip, local_poh, row["reject_pct"], unpaid, prim_bal, leg_bal, combined, row["primary_covers_unpaid"]))
if hard:
    print("[d1-soak] HARD:", ", ".join(hard), file=sys.stderr)
if soft:
    print("[d1-soak] WARN:", ", ".join(soft), file=sys.stderr)
if hard:
    raise SystemExit(1)
if strict and soft:
    raise SystemExit(2)
print("[d1-soak] OK")
PY
