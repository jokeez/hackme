#!/usr/bin/env bash
# Gate: /api/hardware/tune + rig-profiles + CPU cap POST round-trip.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
BASE="${HACKME_BASE_URL:-http://127.0.0.1:8080}"
ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "[hardware-gate] missing $ENV_FILE" >&2
  exit 2
fi
# shellcheck disable=SC1090
set -a && source "$ENV_FILE" && set +a
if [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
  echo "[hardware-gate] HACKME_ADMIN_TOKEN unset" >&2
  exit 2
fi

curl -fsS --max-time 8 "$BASE/api/status?lite=1" >/dev/null || {
  echo "[hardware-gate] node not up at $BASE" >&2
  exit 1
}

echo "[hardware-gate] GET /api/hardware/tune"
j="$(curl -fsS "$BASE/api/hardware/tune")"
echo "$j" | jq -e '.cpu.min_pct != null and (.devices | type) == "array"' >/dev/null

echo "[hardware-gate] GET rig-profiles + detect"
curl -fsS "$BASE/api/hardware/rig-profiles" | jq -e '.profiles | length > 0' >/dev/null
curl -fsS "$BASE/api/hardware/rig-profiles/detect" | jq -e '.gpu_names != null' >/dev/null

echo "[hardware-gate] POST CPU soft_cap_pct 77"
curl -fsS -X POST "$BASE/api/hardware/tune" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
  -d '{"soft_cap_pct":77}' | jq -e '.ok == true and .soft_cap_pct == 77' >/dev/null

j2="$(curl -fsS "$BASE/api/hardware/tune")"
echo "$j2" | jq -e '.cpu.soft_cap_pct == 77' >/dev/null

echo "[hardware-gate] restore CPU soft_cap_pct 80"
curl -fsS -X POST "$BASE/api/hardware/tune" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
  -d '{"soft_cap_pct":80}' >/dev/null

amd="$(echo "$j2" | jq -r '.amd_telemetry')"
if [[ "$amd" == "true" ]]; then
  echo "[hardware-gate] AMD host: skip NVIDIA power POST"
  echo "$j2" | jq '{amd_telemetry,devices:[.devices[]|{index,name,util_pct}]}'
else
  echo "[hardware-gate] NVIDIA or no GPU telemetry path OK"
fi

echo "[hardware-gate] OK"
