#!/usr/bin/env bash
# Show Zero-Knowledge wallet banner once per boot (TTY + serial console).
set -euo pipefail

ZK_JSON="${1:-/run/hackme-os/zk-wallet.json}"
[[ -f "$ZK_JSON" ]] || exit 0

WALLET="$(jq -r '.wallet // empty' "$ZK_JSON" 2>/dev/null || true)"
PHRASE="$(jq -r '.recovery_phrase // empty' "$ZK_JSON" 2>/dev/null || true)"
INI="$(jq -r '.ini_path // empty' "$ZK_JSON" 2>/dev/null || true)"
[[ -n "$WALLET" ]] || exit 0

banner() {
  cat <<BAN
╔══════════════════════════════════════════════════════════════════════════╗
║                    HACKME OS — ZERO-KNOWLEDGE START                      ║
╠══════════════════════════════════════════════════════════════════════════╣
║  [WALLET GENERATED] Your rig mines to:                                   ║
║       ${WALLET}
╠══════════════════════════════════════════════════════════════════════════╣
║  [IMPORTANT] Write down your RECOVERY PHRASE to claim your HMC later:    ║
║
BAN
  if [[ -n "$PHRASE" ]]; then
    # Wrap phrase for 72-char terminal width.
    echo "$PHRASE" | fold -s -w 68 | sed 's/^/║    /'
  else
    echo "║    (phrase unavailable — see /var/lib/hackme/miner.env seed hex)" >&2
  fi
  cat <<BAN2
║                                                                          ║
║  Saved to: ${INI:-/var/lib/hackme/hackme.ini}                            ║
║  Mining starts automatically. Commands: hackme-os-status · hackme-show-wallet ║
╚══════════════════════════════════════════════════════════════════════════╝
BAN2
}

for tty in /dev/tty1 /dev/ttyS0 /dev/console; do
  [[ -w "$tty" ]] && banner >"$tty" 2>/dev/null || true
done
banner | tee -a /var/log/hackme-zk-wallet.log >/dev/console 2>/dev/null || banner | tee -a /var/log/hackme-zk-wallet.log
