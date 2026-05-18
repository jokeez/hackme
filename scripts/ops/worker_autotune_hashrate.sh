#!/usr/bin/env bash
set -euo pipefail

# Autotune worker hashrate_gh_s from a short local PoH benchmark (POST /api/mining/start).
# Requires HACKME_CHAIN_LEADER_LOCAL_POH=1 on the node during the benchmark window.
#
# Usage:
#   ADMIN_TOKEN=... bash scripts/ops/worker_autotune_hashrate.sh
# Optional:
#   BASE=http://127.0.0.1:8080 SAMPLE_SEC=24 APPLY=1

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
SAMPLE_SEC="${SAMPLE_SEC:-24}"
APPLY="${APPLY:-1}"
RUN_ID="${RUN_ID:-worker_autotune_$(date -u +%Y%m%dT%H%M%SZ)}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "worker_autotune_hashrate: ADMIN_TOKEN is required" >&2
  exit 2
fi

out_dir="reports/tests/${RUN_ID}"
mkdir -p "$out_dir"

status_json="$(curl -x "" -sS "${BASE}/api/status")"
echo "$status_json" > "${out_dir}/status_before.json"
solo_allowed="$(echo "$status_json" | jq -r '.local_solo_allowed // false')"
if [[ "$solo_allowed" != "true" ]]; then
  echo "worker_autotune_hashrate: local PoH is disabled. Start the node with HACKME_CHAIN_LEADER_LOCAL_POH=1 for the benchmark window." >&2
  exit 3
fi

echo "[autotune] stopping worker for isolated benchmark"
curl -x "" -sS -X POST "${BASE}/api/worker/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null || true
curl -x "" -sS -X POST "${BASE}/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null || true

echo "[autotune] starting local PoH benchmark for ${SAMPLE_SEC}s"
curl -x "" -sS -X POST "${BASE}/api/mining/start" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" > "${out_dir}/mining_start.json"

samples_file="${out_dir}/samples.jsonl"
: > "$samples_file"
start_ts="$(date +%s)"
while true; do
  now_ts="$(date +%s)"
  elapsed=$((now_ts - start_ts))
  if (( elapsed >= SAMPLE_SEC )); then
    break
  fi
  curl -x "" -sS "${BASE}/api/metrics" >> "$samples_file"
  echo >> "$samples_file"
  sleep 2
done

curl -x "" -sS -X POST "${BASE}/api/mining/stop" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null || true

avg_aps="$(jq -rs '[.[] | (.mining_attempts_per_sec // 0) | tonumber | select(. > 0)] | if length>0 then (add/length) else 0 end' "$samples_file")"
avg_ghs="$(jq -nr --arg aps "$avg_aps" '($aps|tonumber)/1000000000')"
rec_ghs="$(jq -nr --arg g "$avg_ghs" 'if ($g|tonumber) < 0.1 then 0.1 else ($g|tonumber) end')"

if [[ "$APPLY" == "1" ]]; then
  echo "[autotune] applying worker hashrate_gh_s=${rec_ghs}"
  curl -x "" -sS -X POST "${BASE}/api/worker/start" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"hashrate_gh_s\": ${rec_ghs}}" > "${out_dir}/worker_start.json"
else
  echo "[autotune] APPLY=0, benchmark only"
fi

jq -n \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --argjson sample_sec "${SAMPLE_SEC}" \
  --argjson avg_attempts_per_sec "${avg_aps}" \
  --argjson recommended_hashrate_gh_s "${rec_ghs}" \
  --argjson apply "$([[ "$APPLY" == "1" ]] && echo true || echo false)" \
  '{
    test: "worker_autotune_hashrate_v1",
    run_id: $run_id,
    base: $base,
    sample_sec: $sample_sec,
    avg_attempts_per_sec: $avg_attempts_per_sec,
    recommended_hashrate_gh_s: $recommended_hashrate_gh_s,
    apply: $apply
  }' > "${out_dir}/summary.json"

cat "${out_dir}/summary.json"
echo "worker_autotune_hashrate: summary ${out_dir}/summary.json"
