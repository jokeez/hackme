#!/usr/bin/env bash
# Host-level sanity before settlement / cron (run on VPS as root or via SSH).
set -euo pipefail

fail=0
ok() { echo "[host-sanity] OK $*"; }
bad() { echo "[host-sanity] FAIL $*" >&2; fail=1; }

if [[ -c /dev/null ]]; then
  ok "/dev/null is char device"
else
  bad "/dev/null is not a character device ($(ls -la /dev/null 2>/dev/null || echo missing))"
  echo "[host-sanity] fix: sudo rm -f /dev/null && sudo mknod -m 666 /dev/null c 1 3" >&2
fi

for cmd in curl jq python3; do
  if command -v "$cmd" >/dev/null 2>&1; then
    ok "command $cmd"
  else
    bad "missing $cmd"
  fi
done

if id hackme >/dev/null 2>&1; then
  if sudo -u hackme bash -c 'command -v curl >/dev/null && echo ok' 2>/dev/null | grep -q ok; then
    ok "hackme user can use /dev/null redirects"
  else
    bad "hackme user cannot redirect to /dev/null"
  fi
fi

exit "$fail"
