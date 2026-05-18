#!/usr/bin/env bash
set -euo pipefail

# Smoke test for GPU power-limit control path.
#
# Usage:
#   BASE=http://127.0.0.1:8080 ADMIN_TOKEN=... GPU_INDEX=0 bash scripts/tests/gpu_power_smoke.sh

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
GPU_INDEX="${GPU_INDEX:-0}"
RUN_ID="${RUN_ID:-gpu_power_smoke_$(date -u +%Y%m%dT%H%M%SZ)}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "gpu_power_smoke: ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 2
fi

out_dir="reports/tests/${RUN_ID}"
mkdir -p "$out_dir"

tune_json="$(curl -x "" -sS "${BASE}/api/hardware/tune")"
echo "${tune_json}" > "${out_dir}/hardware_tune.json"

lim_now="$(echo "${tune_json}" | jq -r --argjson idx "${GPU_INDEX}" '.devices[]? | select(.index == $idx) | .power_limit_w' | head -n 1)"
eco_w="$(echo "${tune_json}" | jq -r --argjson idx "${GPU_INDEX}" '.devices[]? | select(.index == $idx) | .preset_eco_w' | head -n 1)"
daily_w="$(echo "${tune_json}" | jq -r --argjson idx "${GPU_INDEX}" '.devices[]? | select(.index == $idx) | .preset_daily_w' | head -n 1)"
min_w="$(echo "${tune_json}" | jq -r --argjson idx "${GPU_INDEX}" '.devices[]? | select(.index == $idx) | .power_min_w' | head -n 1)"
max_w="$(echo "${tune_json}" | jq -r --argjson idx "${GPU_INDEX}" '.devices[]? | select(.index == $idx) | .power_max_w' | head -n 1)"

if [[ -z "${lim_now}" || "${lim_now}" == "null" ]]; then
  echo "gpu_power_smoke: GPU index ${GPU_INDEX} not found or power info unavailable" >&2
  exit 3
fi
if [[ -z "${eco_w}" || "${eco_w}" == "null" ]]; then
  eco_w="${lim_now}"
fi
if [[ -z "${daily_w}" || "${daily_w}" == "null" ]]; then
  daily_w="${lim_now}"
fi
if [[ -n "${min_w}" && "${min_w}" != "null" ]]; then
  if awk "BEGIN {exit !(${eco_w} < ${min_w})}"; then eco_w="${min_w}"; fi
  if awk "BEGIN {exit !(${daily_w} < ${min_w})}"; then daily_w="${min_w}"; fi
fi
if [[ -n "${max_w}" && "${max_w}" != "null" ]]; then
  if awk "BEGIN {exit !(${eco_w} > ${max_w})}"; then eco_w="${max_w}"; fi
  if awk "BEGIN {exit !(${daily_w} > ${max_w})}"; then daily_w="${max_w}"; fi
fi

apply_eco_http="$(curl -x "" -sS -o "${out_dir}/apply_eco.json" -w '%{http_code}' -X POST "${BASE}/api/hardware/tune" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"gpu_index\": ${GPU_INDEX}, \"power_limit_w\": ${eco_w}}")"
apply_eco="$(cat "${out_dir}/apply_eco.json" 2>/dev/null || true)"

sleep 1
apply_daily_http="$(curl -x "" -sS -o "${out_dir}/apply_daily.json" -w '%{http_code}' -X POST "${BASE}/api/hardware/tune" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"gpu_index\": ${GPU_INDEX}, \"power_limit_w\": ${daily_w}}")"
apply_daily="$(cat "${out_dir}/apply_daily.json" 2>/dev/null || true)"

eco_ok="$(echo "${apply_eco}" | jq -r '.ok // false' 2>/dev/null || echo false)"
daily_ok="$(echo "${apply_daily}" | jq -r '.ok // false' 2>/dev/null || echo false)"

jq -n \
  --arg run_id "${RUN_ID}" \
  --arg base "${BASE}" \
  --argjson gpu_index "${GPU_INDEX}" \
  --argjson initial_limit_w "$(printf '%s' "${lim_now}" | jq -R 'tonumber? // 0')" \
  --argjson eco_requested_w "$(printf '%s' "${eco_w}" | jq -R 'tonumber? // 0')" \
  --argjson daily_requested_w "$(printf '%s' "${daily_w}" | jq -R 'tonumber? // 0')" \
  --argjson eco_applied_w "$(echo "${apply_eco}" | jq -r '.applied_power_limit_w // 0')" \
  --argjson daily_applied_w "$(echo "${apply_daily}" | jq -r '.applied_power_limit_w // 0')" \
  --arg eco_http "${apply_eco_http}" \
  --arg daily_http "${apply_daily_http}" \
  --argjson eco_ok "$( [[ "${eco_ok}" == "true" ]] && echo true || echo false )" \
  --argjson daily_ok "$( [[ "${daily_ok}" == "true" ]] && echo true || echo false )" \
  --arg eco_warning "$(echo "${apply_eco}" | jq -r '.warning // ""')" \
  --arg daily_warning "$(echo "${apply_daily}" | jq -r '.warning // ""')" \
  --arg eco_error "$(echo "${apply_eco}" | jq -r '.error // ""')" \
  --arg daily_error "$(echo "${apply_daily}" | jq -r '.error // ""')" \
  '{
    test: "gpu_power_smoke_v1",
    run_id: $run_id,
    base: $base,
    gpu_index: $gpu_index,
    initial_limit_w: $initial_limit_w,
    requests: { eco_w: $eco_requested_w, daily_w: $daily_requested_w },
    observed: { eco_applied_w: $eco_applied_w, daily_applied_w: $daily_applied_w },
    transport: { eco_http: $eco_http, daily_http: $daily_http },
    ok_flags: { eco_ok: $eco_ok, daily_ok: $daily_ok },
    errors: { eco: $eco_error, daily: $daily_error },
    warnings: { eco: $eco_warning, daily: $daily_warning },
    pass: (($eco_ok == true) and ($daily_ok == true))
  }' > "${out_dir}/summary.json"

cat "${out_dir}/summary.json"
echo "gpu_power_smoke: summary ${out_dir}/summary.json"
if ! jq -e '.pass == true' "${out_dir}/summary.json" >/dev/null 2>&1; then
  exit 1
fi
