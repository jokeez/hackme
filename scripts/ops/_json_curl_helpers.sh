# Shared helpers for ops scripts: tolerate truncated/non-JSON HTTP bodies in jq --argjson.
json_sanitize() {
  local raw="${1:-}"
  if [[ -z "$raw" ]]; then
    echo '{}'
    return 0
  fi
  if printf '%s' "$raw" | jq -ec . >/dev/null 2>&1; then
    printf '%s' "$raw" | jq -c .
  else
    echo '{}'
  fi
}

fetch_json() {
  local url="$1" timeout="${2:-20}"
  shift 2 || true
  json_sanitize "$(curl -fsS --max-time "$timeout" "$@" "$url" 2>/dev/null || echo '{}')"
}
