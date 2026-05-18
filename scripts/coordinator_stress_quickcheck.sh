#!/usr/bin/env bash
set -euo pipefail

COORD="${COORD:-http://127.0.0.1:8081}"
WORKER_ID="${WORKER_ID:-worker-quickcheck}"
BATCH="${BATCH:-2000000}"
SLEEP_AFTER_CLAIM="${SLEEP_AFTER_CLAIM:-0}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

echo "== coordinator quickcheck =="
echo "COORD=$COORD WORKER_ID=$WORKER_ID BATCH=$BATCH"

CLAIM_JSON="$(curl -s -X POST "$COORD/api/work/claim" -H "Content-Type: application/json" \
  -d "{\"worker_id\":\"$WORKER_ID\",\"batch_size\":$BATCH}")"
echo "$CLAIM_JSON" | jq

BASE="$(echo "$CLAIM_JSON" | jq -r '.base_nonce')"
SIZE="$(echo "$CLAIM_JSON" | jq -r '.batch_size')"
WORK_ID="$(echo "$CLAIM_JSON" | jq -r '.work_id')"

if [[ "$SLEEP_AFTER_CLAIM" -gt 0 ]]; then
  echo "Sleeping $SLEEP_AFTER_CLAIM sec to test lease expiry..."
  sleep "$SLEEP_AFTER_CLAIM"
fi

echo
echo "-- submit valid (or expired if slept too long) --"
curl -s -X POST "$COORD/api/work/submit" -H "Content-Type: application/json" -d "{
  \"worker_id\":\"$WORKER_ID\",
  \"base_nonce\":$BASE,
  \"batch_size\":$SIZE,
  \"work_id\":\"$WORK_ID\",
  \"attempts\":$((SIZE/2)),
  \"found\":false,
  \"hashrate_gh_s\":100.0
}" | jq

echo
echo "-- submit same range again (expect unknown_or_already_closed_range) --"
curl -s -X POST "$COORD/api/work/submit" -H "Content-Type: application/json" -d "{
  \"worker_id\":\"$WORKER_ID\",
  \"base_nonce\":$BASE,
  \"batch_size\":$SIZE,
  \"work_id\":\"$WORK_ID\",
  \"attempts\":1000,
  \"found\":false
}" | jq

echo
echo "-- stats snapshot --"
curl -s "$COORD/api/work/stats" | jq '{issued_ranges,reissued_ranges,submitted_items,found_hits,expired_leases,unknown_submits,stale_submits,rejected_submits,dedup_submits,accepted_attempts,total_payout_hmc}'
