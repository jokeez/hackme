#!/usr/bin/env bash
# Early tty banner so rigs never show a blank screen during boot.
set -uo pipefail

msg() {
  local tty="$1"
  [[ -w "$tty" ]] || return 0
  {
    printf '\033[H\033[2J\033[1;32m'
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║  HackMe OS — booting (live USB)                          ║"
    echo "║  Pool: https://hackme.tech                               ║"
    echo "║  GRUB: use «recommended» · root pw: /etc/hackme/root-password ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    printf '\033[0m\n'
  } >"$tty" 2>/dev/null || true
}

for t in /dev/tty1 /dev/console; do
  msg "$t"
done
