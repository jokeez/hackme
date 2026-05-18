#!/usr/bin/env bash
set -euo pipefail

# Hybrid signer smoke for coordinator v2 submit path.
#
# Validates:
# 1) unsigned submit fallback still works
# 2) partial signature payload is rejected when hybrid signer is enabled
#
# Usage:
#   COORD_URL=http://127.0.0.1:8081 COORD_TOKEN=... bash scripts/tests/hybrid_signer_smoke.sh
#
# Optional:
#   WORKER_ID=worker-hybrid-smoke
#   REQUIRE_HYBRID=0|1   (default 0)
#   REQUIRE_STRICT=0|1   (default 0, require hybrid_signer_strict=true)

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[hybrid-smoke] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

COORD_URL="${COORD_URL:-http://127.0.0.1:8081}"
COORD_TOKEN="${COORD_TOKEN:-${ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}}}"
WORKER_ID="${WORKER_ID:-worker-hybrid-smoke}"
REQUIRE_HYBRID="${REQUIRE_HYBRID:-0}"
REQUIRE_STRICT="${REQUIRE_STRICT:-0}"

if [[ -z "$COORD_TOKEN" ]]; then
  echo "[hybrid-smoke] coordinator token is required" >&2
  exit 1
fi

post_json() {
  local path="$1"
  local body="$2"
  curl -sS -X POST "${COORD_URL}${path}" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${COORD_TOKEN}" \
    -d "$body"
}

echo "[hybrid-smoke] fetch work stats"
ws="$(curl -fsS "${COORD_URL}/api/work/stats?details=0")"
hybrid_enabled="$(printf '%s' "$ws" | jq -r '.hybrid_signer_enabled // false')"
hybrid_strict="$(printf '%s' "$ws" | jq -r '.hybrid_signer_strict // false')"
echo "[hybrid-smoke] hybrid_signer_enabled=${hybrid_enabled}"
if [[ "$REQUIRE_HYBRID" == "1" && "$hybrid_enabled" != "true" ]]; then
  echo "[hybrid-smoke] ERROR: hybrid signer not enabled on coordinator" >&2
  exit 2
fi
if [[ "$REQUIRE_STRICT" == "1" && "$hybrid_strict" != "true" ]]; then
  echo "[hybrid-smoke] ERROR: hybrid signer strict mode not enabled" >&2
  exit 6
fi

echo "[hybrid-smoke] unsigned submit behavior check"
c1="$(post_json "/api/work/claim" "{\"worker_id\":\"${WORKER_ID}\",\"batch_size\":1000}")"
base1="$(printf '%s' "$c1" | jq -r '.base_nonce')"
batch1="$(printf '%s' "$c1" | jq -r '.batch_size')"
work1="$(printf '%s' "$c1" | jq -r '.work_id')"
s1="$(post_json "/api/work/submit" "{\"worker_id\":\"${WORKER_ID}\",\"base_nonce\":${base1},\"batch_size\":${batch1},\"work_id\":\"${work1}\",\"attempts\":1000}")"
ok1="$(printf '%s' "$s1" | jq -r '.ok // false')"
reason1="$(printf '%s' "$s1" | jq -r '.reason // ""')"
if [[ "$hybrid_strict" == "true" ]]; then
  if [[ "$ok1" != "false" || "$reason1" != "signature_required" ]]; then
    echo "[hybrid-smoke] ERROR: strict mode expected signature_required, got: $s1" >&2
    exit 3
  fi
else
  if [[ "$ok1" != "true" ]]; then
    echo "[hybrid-smoke] ERROR: unsigned fallback submit failed: $s1" >&2
    exit 3
  fi
fi

echo "[hybrid-smoke] partial signature submit should reject only when hybrid is on"
c2="$(post_json "/api/work/claim" "{\"worker_id\":\"${WORKER_ID}\",\"batch_size\":1000}")"
base2="$(printf '%s' "$c2" | jq -r '.base_nonce')"
batch2="$(printf '%s' "$c2" | jq -r '.batch_size')"
work2="$(printf '%s' "$c2" | jq -r '.work_id')"
s2="$(curl -sS -X POST "${COORD_URL}/api/work/submit" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${COORD_TOKEN}" \
  -d "{\"worker_id\":\"${WORKER_ID}\",\"base_nonce\":${base2},\"batch_size\":${batch2},\"work_id\":\"${work2}\",\"attempts\":1000,\"miner_address\":\"HMC-deadbeefdeadbeef\",\"submit_nonce\":1}")"
reason2="$(printf '%s' "$s2" | jq -r '.reason // ""')"
ok2="$(printf '%s' "$s2" | jq -r '.ok // false')"
if [[ "$hybrid_enabled" == "true" ]]; then
  if [[ "$ok2" != "false" || "$reason2" != "missing_signature_fields" ]]; then
    echo "[hybrid-smoke] ERROR: expected missing_signature_fields, got: $s2" >&2
    exit 4
  fi
else
  if [[ "$ok2" != "true" ]]; then
    echo "[hybrid-smoke] ERROR: hybrid disabled but submit failed: $s2" >&2
    exit 5
  fi
fi

echo "[hybrid-smoke] OK"
