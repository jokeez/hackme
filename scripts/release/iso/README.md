# HackMe Miner ISO build

See [docs/HACKME_OS.md](../../../docs/HACKME_OS.md) for operator/miner usage.

```bash
# Prefer: VERSION="$(cat ../../scripts/release/CURRENT_VERSION)"
export HACKME_RELEASE_POOL_MINER_TOKEN="$(cat ../../.secrets/hackme_coordinator_worker_token)"
VERSION=0.1.0-rc16 bash build_hackme_miner_iso.sh
```

## Visual overhaul (before final ISO)

`visual_overhaul.sh` runs automatically during the build:

| Phase | What |
|-------|------|
| **chroot** | Plymouth theme `hackme` — `[ COMPUTE ENGINE INITIALIZING... ]` |
| **iso-tree** | GRUB theme — black + neon `#00ff66`, banner **HACKME BARE-METAL KERNEL LOADING...** |
| **TTY** | `hackme-os-ui.sh` — Zero-Knowledge frame, `hackme-os-status`, `hackme-show-wallet` |

Manual:

```bash
bash visual_overhaul.sh chroot /path/to/chroot
bash visual_overhaul.sh iso-tree /path/to/iso/staging
```

## Files

| File | Role |
|------|------|
| `build_hackme_miner_iso.sh` | Host entry (release tar + Docker) |
| `build_inner.sh` | debootstrap → squashfs → ISO |
| `visual_overhaul.sh` | GRUB + Plymouth + UI install |
| `hackme-os-ui.sh` | Terminal frames / ASCII / status panels |
| `init-worker.sh` | Zero-Knowledge wallet + monumental TTY1 banner |
| `Dockerfile` | Reproducible build image |
| `chroot-install.sh` | Packages + systemd inside chroot |
| `run-miner-worker.sh` | Pool worker (no Go on rig) |
| `overlay/` | `/etc/hackme`, systemd units |
| `assets/grub/` | GRUB2 `theme.txt` |
| `assets/plymouth/` | Plymouth script theme |
