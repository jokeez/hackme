#!/usr/bin/env bash
# Manual/automated DNS failover helper for Cloudflare.
#
# Requires:
#   CF_API_TOKEN (Zone.DNS edit for target zone)
#   CF_ZONE_ID
#   CF_RECORD_ID (A record for hackme.tech)
#   TARGET_IP (mirror IP)
#
# Example:
#   CF_API_TOKEN=... CF_ZONE_ID=... CF_RECORD_ID=... TARGET_IP=103.244.227.134 \
#   bash scripts/ops/cloudflare_dns_failover.sh
set -euo pipefail

CF_API_TOKEN="${CF_API_TOKEN:-}"
CF_ZONE_ID="${CF_ZONE_ID:-}"
CF_RECORD_ID="${CF_RECORD_ID:-}"
TARGET_IP="${TARGET_IP:-}"
RECORD_NAME="${RECORD_NAME:-hackme.tech}"
TTL="${TTL:-300}"
PROXIED="${PROXIED:-false}"

for v in CF_API_TOKEN CF_ZONE_ID CF_RECORD_ID TARGET_IP; do
  if [[ -z "${!v:-}" ]]; then
    echo "missing $v" >&2
    exit 1
  fi
done

payload="$(jq -nc \
  --arg type "A" \
  --arg name "$RECORD_NAME" \
  --arg content "$TARGET_IP" \
  --argjson ttl "$TTL" \
  --argjson proxied "$PROXIED" \
  '{type:$type,name:$name,content:$content,ttl:$ttl,proxied:$proxied}')"

resp="$(curl -fsS -X PUT \
  "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/dns_records/${CF_RECORD_ID}" \
  -H "Authorization: Bearer ${CF_API_TOKEN}" \
  -H "Content-Type: application/json" \
  --data "$payload")"

echo "$resp" | jq '{ok:.success,record:.result|{name,content,ttl,proxied,modified_on}}'
