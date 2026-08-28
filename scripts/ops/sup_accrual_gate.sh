#!/usr/bin/env bash
# sup_accrual_gate.sh — SUP honest-accrual unit tests + optional live coordinator policy check.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "[sup-gate] coordinator unit tests (SUP accrual + anti-abuse)"
go test ./cmd/coordinator/... -run 'SUP|sup' -count=1

COORD_URL="${COORD_URL:-${HACKME_COORD_URL:-}}"
if [[ -n "$COORD_URL" ]]; then
  COORD_URL="${COORD_URL%/}"
  echo "[sup-gate] live policy check: ${COORD_URL}/api/work/stats"
  body="$(curl -fsS --max-time 20 "${COORD_URL}/api/work/stats?details=0" 2>/dev/null || true)"
  if [[ -z "$body" ]]; then
    echo "[sup-gate] WARN: coordinator stats unreachable (skip live check)" >&2
    exit 0
  fi
  if command -v jq >/dev/null 2>&1; then
    enabled="$(echo "$body" | jq -r '.sup_policy.enabled // false')"
    if [[ "$enabled" != "true" ]]; then
      echo "[sup-gate] FAIL: sup_policy.enabled is not true on coordinator" >&2
      exit 1
    fi
    on_chain="$(echo "$body" | jq -r '.sup_policy.on_chain_settle // false')"
    echo "[sup-gate] live: sup_policy.enabled=true on_chain_settle=${on_chain}"
  fi
fi

echo "[sup-gate] OK"
