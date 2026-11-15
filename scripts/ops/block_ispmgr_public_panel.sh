#!/usr/bin/env bash
# Block ISPmanager panel (1500/1501) on the public NIC. Panel bypasses UFW via ispmgr_* chains.
# Prefer binding ihttpd/nginx panel listeners to 127.0.0.1 (SSH tunnel only); DROP is defense-in-depth.
set -euo pipefail
IFACE="${HACKME_PUBLIC_IFACE:-}"
if [[ -z "$IFACE" ]]; then
  IFACE=$(ip -4 route show default | awk '{print $5; exit}')
fi
[[ -n "$IFACE" ]] || exit 0
ensure_drop() {
  local port="$1"
  if ! iptables -C INPUT -i "$IFACE" -p tcp --dport "$port" -j DROP 2>/dev/null; then
    iptables -I INPUT 1 -i "$IFACE" -p tcp --dport "$port" -j DROP
    echo "[harden] DROP $port on $IFACE"
  fi
  # Also drop without interface match (covers hairpin / unexpected NICs).
  if ! iptables -C INPUT -p tcp --dport "$port" ! -i lo -j DROP 2>/dev/null; then
    iptables -I INPUT 1 -p tcp --dport "$port" ! -i lo -j DROP
    echo "[harden] DROP $port non-lo"
  fi
}
ensure_drop 1501
ensure_drop 1500
# Warn if panel still listens on a non-loopback address.
if command -v ss >/dev/null 2>&1; then
  if ss -ltn | awk '$4 ~ /:1500$|:1501$/ && $4 !~ /127\.0\.0\.1/ && $4 !~ /\[::1\]/ {print; found=1} END{exit !found}'; then
    echo "[harden] WARN: panel still listening on non-loopback — bind ihttpd/nginx to 127.0.0.1" >&2
  fi
fi
