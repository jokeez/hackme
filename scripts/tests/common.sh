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

# Shared pass/fail counters for multi-check test suites (gpu_rig_suite, mining_load_suite, …).
test_suite_counters_init() {
  TEST_SUITE_PASSES=0
  TEST_SUITE_FAILURES=0
}

test_suite_record() {
  local status="$1" name="$2" detail="${3:-}"
  if [[ "$status" == "PASS" ]]; then
    TEST_SUITE_PASSES=$((TEST_SUITE_PASSES + 1))
    pass "$name${detail:+ — $detail}"
  else
    TEST_SUITE_FAILURES=$((TEST_SUITE_FAILURES + 1))
    fail_msg "$name${detail:+ — $detail}"
  fi
}

fail_msg() { printf '[FAIL] %s\n' "$*" >&2; }

# Prefer explicit ADMIN_TOKEN, then live local node (.env.desktop), then .secrets file.
resolve_admin_token() {
  local root="${1:-}"
  if [[ -n "${ADMIN_TOKEN:-}" ]]; then
    printf '%s' "$ADMIN_TOKEN"
    return 0
  fi
  if [[ -n "${HACKME_ADMIN_TOKEN:-}" ]]; then
    printf '%s' "$HACKME_ADMIN_TOKEN"
    return 0
  fi
  if [[ -z "$root" ]]; then
    root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  fi
  local desktop="${DESKTOP_ENV_FILE:-$root/.env.desktop}"
  if [[ -f "$desktop" ]]; then
    local tok
    tok="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$desktop" | cut -d= -f2- | tr -d '\r\n')"
    if [[ -n "$tok" ]]; then
      printf '%s' "$tok"
      return 0
    fi
  fi
  local secrets="${ADMIN_FILE:-$root/.secrets/hackme_admin_token}"
  if [[ -f "$secrets" ]]; then
    tr -d '\r\n' <"$secrets"
    return 0
  fi
  return 1
}

# Remove ephemeral Go sources left under reports/ by transfer_demo (pollutes go test ./...).
cleanup_stale_report_go_junk() {
  find reports -type f \( -name 'sign_transfer_tmp.go' -o -name '_sign_transfer.go' \) -delete 2>/dev/null || true
}
