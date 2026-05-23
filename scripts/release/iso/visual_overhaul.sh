#!/usr/bin/env bash
# HackMe OS visual overhaul — GRUB theme, Plymouth, terminal UI hooks.
# Run before final ISO assembly:
#   visual_overhaul.sh chroot /path/to/chroot
#   visual_overhaul.sh iso-tree /path/to/iso/staging
#
# Called from chroot-install.sh and build_inner.sh automatically.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSETS="${ROOT}/assets"

usage() {
  echo "Usage: $0 chroot <chroot_dir>" >&2
  echo "       $0 iso-tree <iso_staging_dir>" >&2
  exit 2
}

install_ui_scripts() {
  local dest="$1/opt/hackme/scripts/release/iso"
  local src="${ROOT}/hackme-os-ui.sh"
  local dst="${dest}/hackme-os-ui.sh"
  mkdir -p "$dest"
  # chroot-install already copied scripts to /opt/hackme/... — skip self-install.
  if [[ ! -f "$src" ]]; then
    return 0
  fi
  if [[ ! -f "$dst" ]]; then
    install -m 0644 "$src" "$dst"
  elif ! cmp -s "$src" "$dst" 2>/dev/null; then
    install -m 0644 "$src" "$dst"
  fi
}

install_plymouth() {
  local chroot="$1"
  local theme_dst="${chroot}/usr/share/plymouth/themes/hackme"
  mkdir -p "$theme_dst"
  install -m 0644 "${ASSETS}/plymouth/hackme.plymouth" "${theme_dst}/hackme.plymouth"
  install -m 0644 "${ASSETS}/plymouth/hackme.script" "${theme_dst}/hackme.script"

  mkdir -p "${chroot}/etc/plymouth"
  cat >"${chroot}/etc/plymouth/plymouthd.conf" <<'PLY'
# HackMe OS — custom boot splash
[Daemon]
Theme=hackme
ShowDelay=0
DeviceTimeout=8
PLY

  if [[ -f "${chroot}/etc/default/grub" ]]; then
    if ! grep -q 'splash' "${chroot}/etc/default/grub" 2>/dev/null; then
      sed -i 's/^GRUB_CMDLINE_LINUX_DEFAULT="/GRUB_CMDLINE_LINUX_DEFAULT="quiet splash /' \
        "${chroot}/etc/default/grub" 2>/dev/null || true
    fi
  fi

  if command -v update-alternatives >/dev/null 2>&1; then
    update-alternatives --install /usr/share/plymouth/themes/default.plymouth default.plymouth \
      /usr/share/plymouth/themes/hackme/hackme.plymouth 200 2>/dev/null || true
    update-alternatives --set default.plymouth /usr/share/plymouth/themes/hackme/hackme.plymouth 2>/dev/null || true
  fi
}

install_grub_theme_iso() {
  local iso="$1"
  local theme_dst="${iso}/boot/grub/themes/hackme"
  mkdir -p "$theme_dst"
  install -m 0644 "${ASSETS}/grub/theme.txt" "${theme_dst}/theme.txt"

  # Optional: bundle DejaVu from host for gfxterm menu (best-effort)
  local font_dir=""
  for d in /usr/share/grub /boot/grub; do
    [[ -d "${d}/DejaVuSans-Regular-14.pf2" ]] && font_dir="$d" && break
    [[ -f "${d}/DejaVuSans-Regular-14.pf2" ]] && font_dir="$d" && break
  done
  if [[ -z "$font_dir" ]]; then
    for pf2 in /usr/share/grub/*.pf2 /boot/grub/*.pf2; do
      [[ -f "$pf2" ]] || continue
      cp -f "$pf2" "${theme_dst}/" 2>/dev/null || true
      font_dir=1
      break
    done
  else
    cp -f "${font_dir}"/*.pf2 "${theme_dst}/" 2>/dev/null || true
  fi
}

write_grub_cfg() {
  local iso="$1"
  local cfg="${iso}/boot/grub/grub.cfg"
  mkdir -p "$(dirname "$cfg")"
  cat >"$cfg" <<'GRUB'
set default=0
set timeout=4
set gfxmode=1024x768,auto
set gfxpayload=keep
insmod gfxterm
insmod png
terminal_output gfxterm
if [ -f ($root)/boot/grub/themes/hackme/theme.txt ]; then
  set theme=($root)/boot/grub/themes/hackme/theme.txt
  export theme
fi

menuentry "HackMe OS (live · max performance)" {
  linux /casper/vmlinuz boot=casper toram quiet splash isolcpus=1 nohz_full=1 rcu_nocbs=1 ---
  initrd /casper/initrd
}
menuentry "HackMe OS (live — safe graphics)" {
  linux /casper/vmlinuz boot=casper nomodeset toram quiet splash ---
  initrd /casper/initrd
}
GRUB
}

install_chroot() {
  local chroot="$1"
  echo "[visual-overhaul] chroot plymouth + UI libs"
  install_ui_scripts "$chroot"
  install_plymouth "$chroot"

  # Plymouth into initramfs (chroot-install runs update-initramfs after us)
  if [[ -d "${chroot}/usr/share/plymouth/themes/hackme" ]]; then
    echo "[visual-overhaul] plymouth theme → hackme"
  fi
}

install_iso_tree() {
  local iso="$1"
  echo "[visual-overhaul] ISO GRUB theme + grub.cfg"
  install_grub_theme_iso "$iso"
  write_grub_cfg "$iso"
}

main() {
  [[ $# -ge 2 ]] || usage
  case "$1" in
    chroot)
      install_chroot "$2"
      ;;
    iso-tree)
      install_iso_tree "$2"
      ;;
    *)
      usage
      ;;
  esac
  echo "[visual-overhaul] done ($1)"
}

main "$@"
