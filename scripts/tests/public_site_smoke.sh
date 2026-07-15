#!/usr/bin/env bash
# Curl smoke: hackme.tech pages + ISO headers.
# HTML checks use HEAD (+ short ranged GET) with retries — full-body GET across
# Cloudflare often stalls after a few sequential downloads (not a "heavy page").
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE="${SITE_BASE:-https://hackme.tech}"
ISO_VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_ISO_VERSION" 2>/dev/null || echo 0.1.0-rc11s)"
ISO_URL="${ISO_URL:-$SITE/dist/release_${ISO_VER}/HackMe-OS-${ISO_VER}-amd64.iso}"
CURL_MAX_TIME="${CURL_MAX_TIME:-25}"
CURL_RETRIES="${CURL_RETRIES:-4}"
CURL_RETRY_SLEEP="${CURL_RETRY_SLEEP:-1.5}"

fail=0

# Return 0 if HTTP code is success for a static asset (200 / 301 / 302 / 304).
_ok_code() {
  case "$1" in
    200|301|302|304) return 0 ;;
    *) return 1 ;;
  esac
}

# curl with retries; prints http_code to stdout; body optional via $2 dest file.
_curl_code() {
  local url="$1"
  local dest="${2:-/dev/null}"
  local extra=( "${@:3}" )
  local attempt=1
  local code=""
  local ec=0
  while [[ "$attempt" -le "$CURL_RETRIES" ]]; do
    code="$(curl -sS -o "$dest" -w '%{http_code}' --max-time "$CURL_MAX_TIME" \
      -H 'Accept-Encoding: identity' \
      -H 'Cache-Control: no-cache' \
      "${extra[@]}" \
      "$url" 2>/dev/null)" && ec=0 || ec=$?
    if [[ "$ec" -eq 0 ]] && _ok_code "$code"; then
      printf '%s' "$code"
      return 0
    fi
    # Retry transient stalls / CF edge timeouts (28) and empty codes.
    if [[ "$ec" -eq 28 || "$ec" -eq 52 || "$ec" -eq 56 || -z "$code" || "$code" == "000" || "$code" == "522" || "$code" == "524" ]]; then
      sleep "$CURL_RETRY_SLEEP"
      attempt=$((attempt + 1))
      continue
    fi
    printf '%s' "${code:-000}"
    return 1
  done
  printf '%s' "${code:-000}"
  return 1
}

check_html() {
  local path="$1"
  local needle="${2:-}"
  local url="$SITE$path"
  local code tmp
  tmp="$(mktemp)"

  # Soft pacing — sequential full downloads from one IP trigger CF/edge stalls.
  sleep 0.4

  # 1) HEAD — fast liveness (nginx/CF must answer without streaming full body).
  if code="$(_curl_code "$url" /dev/null -I -X HEAD)"; then
    :
  else
    # Some origins disallow HEAD; fall through to ranged GET.
    code="000"
  fi

  if ! _ok_code "$code"; then
    # 2) Ranged GET — first 8 KiB only (enough for title / markers).
    if ! code="$(_curl_code "$url" "$tmp" -r 0-8191)"; then
      echo "[site-smoke] FAIL $path HTTP ${code} (after ${CURL_RETRIES} retries)" >&2
      rm -f "$tmp"
      fail=$((fail + 1))
      return
    fi
  else
    # Confirm a shred of content when HEAD ok (optional needle).
    _curl_code "$url" "$tmp" -r 0-4095 >/dev/null || true
  fi

  if [[ -n "$needle" && -s "$tmp" ]] && ! grep -qi "$needle" "$tmp"; then
    echo "[site-smoke] FAIL $path missing marker '$needle'" >&2
    rm -f "$tmp"
    fail=$((fail + 1))
    return
  fi
  rm -f "$tmp"
  echo "[site-smoke] PASS $path"
}

check_html "/" "HackMe"
check_html "/index.html" "HackMe"
check_html "/downloads.html" "download"
check_html "/contacts.html" "Contact"
check_html "/security-rewards.html" "security"

if [[ "${SKIP_ISO:-0}" == "1" ]]; then
  echo "[site-smoke] SKIP ISO check (SKIP_ISO=1)"
else
len="$(curl -sS -I --max-time "$CURL_MAX_TIME" -H 'Accept-Encoding: identity' "$ISO_URL" 2>/dev/null \
  | grep -i '^content-length:' | awk '{print $2}' | tr -d '\r' || true)"
if [[ -z "$len" ]]; then
  # Some nginx configs omit Content-Length on HEAD; probe first byte via Range.
  hdr="$(curl -sS -I --max-time "$CURL_MAX_TIME" -H 'Accept-Encoding: identity' -r 0-0 "$ISO_URL" 2>/dev/null \
    | awk 'BEGIN{IGNORECASE=1} /^Content-Range:/ {print $0}' || true)"
  if echo "$hdr" | grep -q '/'; then
    len="$(echo "$hdr" | sed -n 's|.*/\([0-9]*\).*|\1|p')"
  fi
fi
if [[ -n "$len" && "$len" -gt 800000000 ]]; then
  echo "[site-smoke] PASS ISO size=$len bytes"
else
  # One soft retry for ISO HEAD (CDN cold)
  sleep "$CURL_RETRY_SLEEP"
  len="$(curl -sS -I --max-time "$CURL_MAX_TIME" -H 'Accept-Encoding: identity' "$ISO_URL" 2>/dev/null \
    | grep -i '^content-length:' | awk '{print $2}' | tr -d '\r' || true)"
  if [[ -n "$len" && "$len" -gt 800000000 ]]; then
    echo "[site-smoke] PASS ISO size=$len bytes"
  else
    echo "[site-smoke] FAIL ISO size (${len:-missing}; expected >800MB) url=$ISO_URL" >&2
    fail=$((fail + 1))
  fi
fi
fi

if [[ "$fail" -gt 0 ]]; then
  exit 1
fi
echo "[site-smoke] all checks passed"
