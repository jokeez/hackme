#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
SNAP_DIR="$OUT_DIR/$RID/baseline"

ensure_reports_dir "$SNAP_DIR"

status_json="$(json_get "$BASE/api/status")"
metrics_json="$(json_get "$BASE/api/metrics")"
wallet_json="$(json_get "$BASE/api/wallet")"
network_json="$(json_get "$BASE/api/network/stats")"

printf '%s\n' "$status_json" >"$SNAP_DIR/status.json"
printf '%s\n' "$metrics_json" >"$SNAP_DIR/metrics.json"
printf '%s\n' "$wallet_json" >"$SNAP_DIR/wallet.json"
printf '%s\n' "$network_json" >"$SNAP_DIR/network_stats.json"

schema_v="$(jq -r '.schema_version // -1' <<<"$status_json")"
schema_exp="$(jq -r '.schema_expected // -1' <<<"$status_json")"
tip_h="$(jq -r '.tip_height // 0' <<<"$status_json")"
minted="$(jq -r '.economics.total_minted_hmc // 0' <<<"$status_json")"
burned="$(jq -r '.economics.total_burned_hmc // 0' <<<"$status_json")"
circ="$(jq -r '.economics.circulating_hmc // 0' <<<"$status_json")"
target_mod="$(jq -r '.mining_target_mod // 0' <<<"$metrics_json")"
at_cap="$(jq -r '.mining_target_mod_at_cap // false' <<<"$metrics_json")"
obs_sec="$(jq -r '.mining_observed_block_sec // -1' <<<"$metrics_json")"

{
  echo "run_id=$RID"
  echo "captured_at=$(ts_utc)"
  echo "base=$BASE"
  echo "tip_height=$tip_h"
  echo "schema=$schema_v/$schema_exp"
  echo "minted=$minted burned=$burned circulating=$circ"
  echo "target_mod=$target_mod at_cap=$at_cap observed_block_sec=$obs_sec"
} >"$SNAP_DIR/summary.txt"

if [[ "$schema_v" != "$schema_exp" ]]; then
  fail "schema mismatch ($schema_v != $schema_exp). Snapshot: $SNAP_DIR"
fi

pass "baseline snapshot captured in $SNAP_DIR"
