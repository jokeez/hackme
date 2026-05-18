#!/usr/bin/env bash
set -euo pipefail

# Probe configured P2P peers for reachability/health diagnosis.
#
# Usage:
#   BASE=http://127.0.0.1:8080 scripts/ops/p2p_peer_probe.sh
# Optional:
#   RUN_ID=p2p_probe_... TIMEOUT_SEC=3

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
RUN_ID="${RUN_ID:-p2p_probe_$(date -u +%Y%m%dT%H%M%SZ)}"
TIMEOUT_SEC="${TIMEOUT_SEC:-3}"

out_dir="${ROOT_DIR}/reports/gates/${RUN_ID}"
mkdir -p "${out_dir}"

peers_json="${out_dir}/p2p_peers.json"
results_jsonl="${out_dir}/probe_results.jsonl"
summary_json="${out_dir}/p2p_peer_probe_summary.json"

curl -x "" -fsS "${BASE}/api/p2p/peers" >"${peers_json}"
: >"${results_jsonl}"

enabled="$(jq -r '.enabled // false' "${peers_json}")"
if [[ "${enabled}" != "true" ]]; then
  jq -nc \
    --arg run_id "${RUN_ID}" \
    --arg base "${BASE}" \
    --arg status "PASS" \
    --arg reason "p2p_disabled" \
    '{gate:"p2p_peer_probe_v1",run_id:$run_id,base:$base,status:$status,reason:$reason,total:0,reachable:0}' >"${summary_json}"
  cat "${summary_json}"
  exit 0
fi

mapfile -t peers < <(jq -r '.peers[]?.peer_url // empty' "${peers_json}")
reachable=0
total=0

for peer in "${peers[@]:-}"; do
  [[ -z "${peer}" ]] && continue
  total=$((total + 1))
  code="$(curl -x "" -sS -o /dev/null -w '%{http_code}' --max-time "${TIMEOUT_SEC}" "${peer}/api/p2p/peers" || true)"
  verdict="fail"
  detail="unreachable"
  if [[ "${code}" == "200" || "${code}" == "401" || "${code}" == "403" ]]; then
    verdict="pass"
    detail="reachable_http_${code}"
    reachable=$((reachable + 1))
  fi
  jq -nc --arg peer "${peer}" --arg verdict "${verdict}" --arg detail "${detail}" --arg code "${code:-0}" \
    '{peer_url:$peer,verdict:$verdict,detail:$detail,http_code:$code}' >>"${results_jsonl}"
done

status="PASS"
if (( total > 0 && reachable == 0 )); then
  status="FAIL"
fi

jq -nc \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --arg status "${status}" \
  --argjson total "${total}" \
  --argjson reachable "${reachable}" \
  '{gate:"p2p_peer_probe_v1",run_id:$run_id,base:$base,status:$status,total:$total,reachable:$reachable,results_path:"probe_results.jsonl",peers_path:"p2p_peers.json"}' >"${summary_json}"

cat "${summary_json}"
if [[ "${status}" != "PASS" ]]; then
  echo "p2p_peer_probe: FAIL (summary: ${summary_json})" >&2
  exit 1
fi
echo "p2p_peer_probe: PASS (summary: ${summary_json})"
