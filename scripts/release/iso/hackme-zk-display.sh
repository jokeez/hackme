#!/usr/bin/env bash
# Show Zero-Knowledge wallet banner once per boot (TTY + serial console).
# Security: never append the recovery phrase to world-readable /var/log.
# Persist phrase only under /var/lib/hackme (0600) via init-worker.sh.
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
  # Full banner (wallet + phrase) for local TTY only — operator photographing the screen.
  hackme_ui_zk_tty_banner "$WALLET" "$PHRASE" "${POOL:-https://hackme.tech/pool/coordinator}" "$VER"
}

render_public() {
  # Log/serial: wallet + pool only — never the recovery phrase.
  hackme_ui_zk_tty_banner "$WALLET" "" "${POOL:-https://hackme.tech/pool/coordinator}" "$VER"
}

# Local interactive TTYs may show the phrase (physical console).
for tty in /dev/tty1 /dev/ttyS0; do
  [[ -w "$tty" ]] && render >"$tty" 2>/dev/null || true
done

# Persist a redacted copy under root-only log (no phrase).
log_dir="/var/lib/hackme"
mkdir -p "$log_dir"
logf="${log_dir}/zk-wallet-display.log"
umask 077
render_public >>"$logf" 2>/dev/null || true
chmod 600 "$logf" 2>/dev/null || true

# /dev/console and residual tee paths: redacted only.
render_public >/dev/console 2>/dev/null || true
