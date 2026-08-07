#!/usr/bin/env bash
# Block ISPmanager panel (1500/1501) on the public NIC. Panel bypasses UFW via ispmgr_* chains.
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
}
ensure_drop 1501
ensure_drop 1500
