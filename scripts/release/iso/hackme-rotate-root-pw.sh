#!/usr/bin/env bash
# The ISO is public: a root password set at image build time is the same for everyone who
# downloads it (and survives hackme-os-install). Give each machine its own password on first
# boot. Live sessions get a fresh one on every boot (the overlay is discarded at shutdown).
set -uo pipefail
MARK=/etc/hackme/.root-password-rotated
PWFILE=/etc/hackme/root-password
[[ -f "$MARK" ]] && exit 0
pw="$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | cut -c1-24)"
[[ ${#pw} -ge 16 ]] || { echo "hackme-rotate-root-pw: could not generate a password" >&2; exit 1; }
umask 077
mkdir -p /etc/hackme
echo "root:${pw}" | chpasswd || exit 1
printf '%s\n' "$pw" >"$PWFILE"
chmod 600 "$PWFILE"
touch "$MARK"
echo "hackme-rotate-root-pw: new per-machine root password in $PWFILE"
