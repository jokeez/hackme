# HackMe Miner ISO

Bootable **live USB** image that sends **100% of rig hashpower** to the public HackMe pool (`https://hackme.tech/pool/coordinator`). No local chain mining, no desktop wizard — systemd keeps `workerpoh` alive with restart.

## Build (operator)

```bash
# Pool worker token (same as Windows pool.miner.token)
export HACKME_RELEASE_POOL_MINER_TOKEN="$(cat .secrets/hackme_coordinator_worker_token)"

VERSION=0.1.0-rc11g bash scripts/release/iso/build_hackme_miner_iso.sh
```

Output: `dist/release_<VERSION>/HackMe-Miner-<VERSION>-amd64.iso`

Requires **Docker** (default) or host `debootstrap` + `squashfs-tools` + `xorriso` + `grub-mkrescue`.

## Flash USB

```bash
sudo dd if=dist/release_0.1.0-rc11g/HackMe-Miner-0.1.0-rc11g-amd64.iso of=/dev/sdX bs=4M status=progress conv=fsync
```

Replace `/dev/sdX` with your USB device (not a partition).

## First boot

1. Boot from USB (UEFI or legacy).
2. First boot generates **unique** `WORKER_ID` + `HACKME_MINER_ED25519_SEED_HEX` → `/var/lib/hackme/miner.env`.
3. Worker service starts automatically; **tty1** shows `hackme-miner-status` every 30s.

## Production rigs (persistent disk)

Live session without install loses identity on reboot. For farms:

```bash
sudo hackme-miner-install-disk /dev/sda
```

Then boot from SSD and remove USB.

## GPU notes

| Vendor | ISO live |
|--------|----------|
| AMD / Intel | OpenCL (`ocl-icd`) — usually works OOTB |
| NVIDIA | Install proprietary driver after disk install, then `HACKME_GPU_BACKEND=cuda` in `/var/lib/hackme/miner.env` |

## Verify pool

```bash
hackme-miner-status
journalctl -u hackme-miner-worker -f
curl -sS https://hackme.tech/pool/coordinator/api/work/stats | jq '.summary'
```

Register payout wallet on coordinator for your `WORKER_ID` (same as Linux `.env.worker` fleet docs).

## Related

- Windows miners: `HackMe-Setup-*.exe`
- Linux tarball: `hackme_*_linux.tar.gz` + `install_hackme.sh`
- Fair pool: `scripts/ops/apply_miner_fair_pool.sh`
