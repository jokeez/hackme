#!/usr/bin/env bash
set -euo pipefail

# HackMe invariant checker for manual/private-testnet runs.
# Usage:
#   bash scripts/check_invariants.sh
# Optional env for transfer invariants:
#   BASE=http://127.0.0.1:8080 \
#   TX_HASH=... FROM=HMC-... TO=HMC-... EXPECT_AMOUNT=100000 EXPECT_FEE=1000 \
#   bash scripts/check_invariants.sh
# Optional env for order invariants:
#   ORDER_ID=order-demo-1 EXPECT_REWARD_HMC=0.02 EXPECT_TARGET_SOLVES=3 EXPECT_DIFFICULTY=10

BASE="${BASE:-http://127.0.0.1:8080}"

pass() { printf '[PASS] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

json_get() {
  local url="$1"
  curl -fsS "$url"
}

eq() {
  local got="$1" exp="$2" msg="$3"
  if [[ "$got" != "$exp" ]]; then
    fail "$msg (got=$got expected=$exp)"
  fi
}

require_cmd curl
require_cmd jq
require_cmd python3

echo "== HackMe invariant check =="
echo "BASE: $BASE"

status_json="$(json_get "$BASE/api/status")"
has_genesis="$(jq -r '.has_genesis' <<<"$status_json")"
tip_height="$(jq -r '.tip_height' <<<"$status_json")"
if [[ "$has_genesis" != "true" ]]; then
  fail "has_genesis=false (node is not initialized)"
fi
pass "status reachable; genesis present; tip_height=$tip_height"

econ_json="$(jq -c '.economics // {}' <<<"$status_json")"
if [[ "$econ_json" == "{}" ]]; then
  warn "economics missing in /api/status (skip economics invariants)"
else
  python3 - "$econ_json" <<'PY'
import json, sys, math
e = json.loads(sys.argv[1])
mx = float(e.get("max_supply_hmc", 0.0))
minted = float(e.get("total_minted_hmc", 0.0))
burned = float(e.get("total_burned_hmc", 0.0))
circ = float(e.get("circulating_hmc", 0.0))
remain = float(e.get("mint_remaining_hmc", 0.0))
eps = 1e-6
if minted < -eps or burned < -eps or circ < -eps or remain < -eps:
    raise SystemExit("negative economics value")
if abs((minted - burned) - circ) > 1e-5:
    raise SystemExit(f"circulating mismatch: minted-burned={minted-burned} circ={circ}")
if abs((mx - minted) - remain) > 1e-5:
    raise SystemExit(f"mint_remaining mismatch: max-minted={mx-minted} remain={remain}")
print("OK")
PY
  pass "economics invariants hold (circulating/mint_remaining)"
fi

# Optional transfer invariants when tx context is provided.
if [[ -n "${TX_HASH:-}" ]]; then
  [[ -n "${FROM:-}" ]] || fail "TX_HASH set but FROM is empty"
  [[ -n "${TO:-}" ]] || fail "TX_HASH set but TO is empty"
  [[ -n "${EXPECT_AMOUNT:-}" ]] || fail "TX_HASH set but EXPECT_AMOUNT is empty"
  [[ -n "${EXPECT_FEE:-}" ]] || fail "TX_HASH set but EXPECT_FEE is empty"

  tx_json="$(json_get "$BASE/api/tx/$TX_HASH")"
  tx_status="$(jq -r '.status // empty' <<<"$tx_json")"
  [[ -n "$tx_status" ]] || fail "tx not found by hash: $TX_HASH"
  if [[ "$tx_status" != "included" ]]; then
    warn "tx is not included yet (status=$tx_status); skipping strict transfer invariants"
  else
    tx_from="$(jq -r '.from' <<<"$tx_json")"
    tx_to="$(jq -r '.to' <<<"$tx_json")"
    tx_amount="$(jq -r '.amount_units' <<<"$tx_json")"
    tx_fee="$(jq -r '.fee_units' <<<"$tx_json")"
    eq "$tx_from" "$FROM" "tx.from mismatch"
    eq "$tx_to" "$TO" "tx.to mismatch"
    eq "$tx_amount" "$EXPECT_AMOUNT" "tx.amount_units mismatch"
    eq "$tx_fee" "$EXPECT_FEE" "tx.fee_units mismatch"
    pass "tx payload matches expected transfer context"
  fi
fi

# Optional order invariants when ORDER_ID is provided.
if [[ -n "${ORDER_ID:-}" ]]; then
  tasks_json="$(json_get "$BASE/api/tasks")"
  row_json="$(jq -c --arg id "$ORDER_ID" '.tasks[]? | select(.id == $id)' <<<"$tasks_json")"
  [[ -n "$row_json" ]] || fail "order not found in /api/tasks: $ORDER_ID"

  if [[ -n "${EXPECT_REWARD_HMC:-}" ]]; then
    got_reward="$(jq -r '.reward' <<<"$row_json")"
    eq "$got_reward" "$EXPECT_REWARD_HMC" "order.reward mismatch"
  fi
  if [[ -n "${EXPECT_TARGET_SOLVES:-}" ]]; then
    got_target="$(jq -r '.target_solves' <<<"$row_json")"
    eq "$got_target" "$EXPECT_TARGET_SOLVES" "order.target_solves mismatch"
  fi
  if [[ -n "${EXPECT_DIFFICULTY:-}" ]]; then
    got_diff="$(jq -r '.difficulty_score // 0' <<<"$row_json")"
    eq "$got_diff" "$EXPECT_DIFFICULTY" "order.difficulty_score mismatch"
  fi

  python3 - "$row_json" <<'PY'
import json, sys
r = json.loads(sys.argv[1])
target = int(r.get("target_solves", 0))
progress = int(r.get("progress_count", 0))
pct = float(r.get("progress_pct", 0.0))
if target < 1:
    raise SystemExit("target_solves must be >= 1")
if progress < 0:
    raise SystemExit("progress_count must be >= 0")
if progress > target:
    raise SystemExit("progress_count exceeds target_solves")
expected_pct = (progress / target * 100.0) if target else 0.0
if abs(expected_pct - pct) > 0.2:
    raise SystemExit(f"progress_pct mismatch: expected {expected_pct:.4f}, got {pct:.4f}")
status = str(r.get("status", ""))
if progress >= target and status != "completed":
    raise SystemExit("status should be completed when progress reaches target")
print("OK")
PY
  pass "order progress/status invariants hold for ORDER_ID=$ORDER_ID"
fi

echo "== Completed =="
