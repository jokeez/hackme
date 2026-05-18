#!/usr/bin/env bash
set -euo pipefail

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing command: $1" >&2
    exit 1
  }
}

ts_utc() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

run_id() {
  date -u +"%Y%m%dT%H%M%SZ"
}

ensure_reports_dir() {
  local dir="$1"
  mkdir -p "$dir"
}

# Retry curl on transient TCP failures (busy node / accept backlog after gate bursts).
# Honors CURL_TRANSIENT_RETRIES (default 25) and CURL_TRANSIENT_DELAY_SEC (default 0.35).
curl_retry_fsS() {
  local max="${CURL_TRANSIENT_RETRIES:-25}"
  local delay="${CURL_TRANSIENT_DELAY_SEC:-0.35}"
  local attempt=1 out ec
  while [[ "$attempt" -le "$max" ]]; do
    ec=0
    out="$(curl "$@")" || ec=$?
    if [[ "$ec" -eq 0 ]]; then
      printf '%s' "$out"
      return 0
    fi
    if [[ "$ec" -eq 7 || "$ec" -eq 28 ]]; then
      echo "[curl_retry_fsS] transient curl exit=$ec attempt $attempt/$max" >&2
      sleep "$delay"
      attempt=$((attempt + 1))
      continue
    fi
    return "$ec"
  done
  return 1
}

json_get() {
  local url="$1"
  curl_retry_fsS -fsS "$url"
}

json_post() {
  local url="$1"
  local body="$2"
  local token="${3:-}"
  if [[ -n "$token" ]]; then
    curl_retry_fsS -fsS -X POST "$url" -H "Content-Type: application/json" -H "X-Hackme-Admin-Token: $token" -d "$body"
  else
    curl_retry_fsS -fsS -X POST "$url" -H "Content-Type: application/json" -d "$body"
  fi
}

pass() { printf '[PASS] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }
