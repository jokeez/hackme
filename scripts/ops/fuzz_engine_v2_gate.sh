#!/usr/bin/env bash
# Smoke gate for fuzz_engine_v2 (local node with admin token).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="${HACKME_ADMIN_TOKEN:-}"
[[ -n "$ADMIN" ]] || ADMIN="$(tr -d '\r\n' < .secrets/hackme_admin_token 2>/dev/null || true)"
if [[ -z "$ADMIN" ]]; then
  echo "[fuzz-v2-gate] HACKME_ADMIN_TOKEN required" >&2
  exit 2
fi

echo "[fuzz-v2-gate] unit tests"
go test -count=1 -run 'TestDeriveFuzzInput|TestNormalizeFuzzCampaignConfig' .

echo "[fuzz-v2-gate] create property campaign"
export CID="fuzz-v2-gate-$(date +%s)"
BODY="$(python3 - <<PY
import json, os
print(json.dumps({
  "id": os.environ["CID"],
  "campaign_type": "property",
  "title": "fuzz engine v2 gate",
  "budget_runs": 40,
  "budget_seconds": 120,
  "config": {
    "fuzz_engine_version": "fuzz_engine_v2",
    "mutation_rounds": 3,
    "seed_corpus": [0, 1, 0x1000000000001, 0x3000000030D40],
  },
}))
PY
)"
CREATE="$(curl -fsS -X POST "$BASE/api/fuzz/campaigns" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$BODY")"
echo "$CREATE" | jq -e '.fuzz_engine.version == "fuzz_engine_v2"' >/dev/null

echo "[fuzz-v2-gate] wait for autorunner progress"
for _ in $(seq 1 30); do
  ST="$(curl -fsS -H "X-Hackme-Admin-Token: $ADMIN" "$BASE/api/fuzz/campaigns/$CID")"
  DONE="$(echo "$ST" | jq -r '.campaign.summary.runs_done // 0')"
  ENG="$(echo "$ST" | jq -r '.campaign.summary.fuzz_engine.version // ""')"
  echo "  runs_done=$DONE engine=$ENG"
  if [[ "$DONE" -ge 20 ]]; then
    break
  fi
  sleep 2
done
echo "$ST" | jq -e '.campaign.summary.runs_done >= 20' >/dev/null
echo "$ST" | jq -e '.campaign.summary.fuzz_engine.seed_count >= 4' >/dev/null

echo "[fuzz-v2-gate] PASS $CID"
