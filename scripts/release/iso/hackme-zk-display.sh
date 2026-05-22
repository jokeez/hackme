#!/usr/bin/env bash
# Show Zero-Knowledge wallet banner once per boot (TTY + serial console).
set -euo pipefail

ZK_JSON="${1:-/run/hackme-os/zk-wallet.json}"
[[ -f "$ZK_JSON" ]] || exit 0

UI="/opt/hackme/scripts/release/iso/hackme-os-ui.sh"
[[ -f "$UI" ]] || exit 0
# shellcheck source=hackme-os-ui.sh
source "$UI"

WALLET="$(jq -r '.wallet // empty' "$ZK_JSON" 2>/dev/null || true)"
PHRASE="$(jq -r '.recovery_phrase // empty' "$ZK_JSON" 2>/dev/null || true)"
POOL="$(jq -r '.pool // empty' "$ZK_JSON" 2>/dev/null || true)"
VER="$(grep -E '^VERSION_ID=' /etc/os-release 2>/dev/null | cut -d= -f2- | tr -d '"' || echo "$HACKME_UI_VERSION")"
[[ -n "$WALLET" ]] || exit 0

render() {
  hackme_ui_zk_tty_banner "$WALLET" "$PHRASE" "${POOL:-https://hackme.tech/pool/coordinator}" "$VER"
}

for tty in /dev/tty1 /dev/ttyS0 /dev/console; do
  [[ -w "$tty" ]] && render >"$tty" 2>/dev/null || true
done
render | tee -a /var/log/hackme-zk-wallet.log >/dev/console 2>/dev/null || render | tee -a /var/log/hackme-zk-wallet.log
