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

python3 - "$tmp" "$OUT_JSONL" "$MAX_REJECT_PCT" "$MAX_FLEET_UNPAID_HMC" \
  "$REQUIRE_FLOAT_COVERS_UNPAID" "$STRICT" "$SETTLEMENT_PRIMARY" "$SETTLEMENT_FLOAT_LEGACY" <<'PY'
import json, sys, time
tmp, out_path, max_rej, max_unpaid, need_cover, strict, prim_addr, leg_addr = sys.argv[1:9]
max_rej = float(max_rej)
max_unpaid = float(max_unpaid)
need_cover = need_cover not in ("0", "false", "False", "")
strict = strict in ("1", "true", "True", "yes")

def load(name):
    with open(f"{tmp}/{name}") as f:
        return json.load(f)

stats, status, settle, prim, leg = map(load, ("stats.json", "status.json", "settle.json", "prim.json", "leg.json"))
ok = float(stats.get("baseline_relay_ok") or 0)
rej = float(stats.get("baseline_relay_reject") or 0)
tot = ok + rej
reject_pct = (100.0 * rej / tot) if tot else None
unpaid = float(settle.get("fleet_unpaid_hmc") or settle.get("total_unpaid_hmc") or 0)
prim_bal = float((prim or {}).get("balance_hmc") or 0)
leg_bal = float((leg or {}).get("balance_hmc") or 0)
local_poh = status.get("chain_leader_local_poh")
tip = status.get("canonical_tip_height") or stats.get("tip_height")
row = {
    "ts": int(time.time()),
    "tip": tip,
    "local_poh": local_poh,
    "relay_mode": stats.get("baseline_relay_mode"),
    "relay_ok": int(ok),
    "relay_reject": int(rej),
    "reject_pct": None if reject_pct is None else round(reject_pct, 3),
    "share_strict": stats.get("share_strict"),
    "share_accepts": stats.get("share_accepts"),
    "pool_gh": stats.get("pool_hashrate_gh_s"),
    "fleet_unpaid_hmc": round(unpaid, 6),
    "settlement_primary": prim_addr,
    "settlement_primary_hmc": round(prim_bal, 8),
    "settlement_legacy_float": leg_addr,
    "settlement_legacy_hmc": round(leg_bal, 8),
    "float_covers_unpaid": prim_bal + 1e-12 >= unpaid,
}
hard, soft = [], []
if local_poh is True:
    hard.append("local_poh_enabled")
if str(stats.get("baseline_relay_mode") or "").lower() not in ("on", "1", "true"):
    hard.append("relay_not_on")
if reject_pct is not None and reject_pct > max_rej:
    hard.append(f"reject_pct>{max_rej}")
if unpaid > max_unpaid:
    hard.append(f"fleet_unpaid>{max_unpaid}")
if stats.get("share_strict") is not True and str(stats.get("share_strict")).lower() not in ("1", "true"):
    soft.append("share_strict_off")
if need_cover and not row["float_covers_unpaid"]:
    soft.append("primary_float_below_unpaid")
row["hard_alerts"] = hard
row["soft_alerts"] = soft
row["ok"] = not hard and (not strict or not soft)
with open(out_path, "a", encoding="utf-8") as f:
    f.write(json.dumps(row, separators=(",", ":")) + "\n")
print("[d1-soak] tip=%s local_poh=%s reject_pct=%s unpaid=%.4f primary=%.4f legacy=%.4f" % (
    tip, local_poh, row["reject_pct"], unpaid, prim_bal, leg_bal))
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
