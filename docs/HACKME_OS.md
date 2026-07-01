# HackMe OS

Bootable **mining rig distribution** (live USB or disk install). All hashpower goes to the public HackMe pool — like HiveOS, but for **HackMe Coin (HMC)** PoH workers.

## Zero-Knowledge Start

Flash the ISO (Balena Etcher, `dd`, etc.), boot the rig — **no wallet setup**.

1. `init-worker.sh` reads `hackme.ini` (empty `wallet=` by default).
2. If no valid `HMC-…` address is set, the rig **locally** generates a new Ed25519 mining key (`minersign -gen-seed`).
3. A **24-word recovery phrase** (BIP39 encoding of the 32-byte seed) and payout address are shown on **TTY1** inside a neon `#00ff66` ASCII frame (photo-worthy Zero-Knowledge banner).
4. Credentials are saved to `/var/lib/hackme/hackme.ini` and mining starts on the public pool automatically.

Commands: `hackme-show-wallet` · `hackme-os-status`

**Important:** The phrase encodes your HackMe **mining** key (HMC payouts), not a Bitcoin/Ethereum HD wallet. Write it on paper to claim mined HMC later. For persistence across live-USB reboots, use `hackme-os-install` or casper writable overlay.

## Features

| Feature | Detail |
|---------|--------|
| **Zero-Knowledge Start** | Empty `hackme.ini` → auto `HMC-…` + recovery phrase + pool worker |
| **CPU isolation** | GRUB: `isolcpus`, `nohz_full`, `rcu_nocbs`; worker on dedicated cores via `taskset` + systemd `CPUAffinity` |
| **Scheduler** | Worker: `SCHED_FIFO` priority 90, `Nice=-20`, realtime I/O |
| **GPU** | AMD: `power_dpm` performance + compute profile; NVIDIA: persistence mode |
| **Rig profiles** | Auto-detect RX 580 2048SP / generic / NVIDIA / Intel Arc → batch sizes from `internal/gputune` |
| **Root** | Login `root` / `hackme` (change with `passwd`) · SSH enabled for farms |
| **Pool** | Preconfigured `https://hackme.tech/pool/coordinator` |

## Build

```bash
export HACKME_RELEASE_POOL_MINER_TOKEN="$(cat .secrets/hackme_coordinator_worker_token)"
VERSION=0.1.0-rc11r bash scripts/release/iso/build_hackme_miner_iso.sh
# Win/Linux + ISO channel: 0.1.0-rc11r — scripts/release/CURRENT_VERSION
```

→ `dist/release_<VERSION>/HackMe-OS-<VERSION>-amd64.iso`

## Flash & boot

```bash
sudo dd if=dist/release_0.1.0-rc11r/HackMe-OS-0.1.0-rc11r-amd64.iso of=/dev/sdX bs=4M status=progress conv=fsync
```

Choose **HackMe OS (live · max performance)** in GRUB.

### Wrong boot screen?

| You see | Meaning |
|---------|---------|
| GRUB **HackMe OS (live …)** then mining status / wallet phrase | Correct ISO |
| **Alpine Linux 3.x** `localhost login:` | **Wrong device or wrong ISO** — not HackMe OS (our image is Ubuntu + casper, not Alpine) |

Before flashing, verify the file:

```bash
bash scripts/tests/verify_hackme_iso.sh /path/to/HackMe-OS-0.1.0-rc11r-amd64.iso
```

Published SHA256: `https://hackme.tech/dist/release_0.1.0-rc11r/SHA256SUMS-iso.txt`

Re-flash with Balena Etcher (verify SHA256), boot **only** that USB, disable other OS entries in BIOS if needed.

## Commands on rig

```bash
hackme-os-status        # pool + GPU + cpus
hackme-os-benchmark 60  # 60s throughput test
hackme-os-install /dev/sda   # persist to SSD
journalctl -u hackme-miner-worker -f
```

## Test tuning on existing Linux (without ISO)

```bash
sudo bash scripts/release/iso/test_host_tune.sh
```

## NVIDIA after disk install

Install proprietary driver, then in `/var/lib/hackme/rig.env` set `HACKME_GPU_BACKEND=cuda` and restart worker.

## Security

Change default root password on production farms. Pool token is embedded at ISO build time — rebuild ISO for rotation, do not publish token.

## Nightly chaos guard

```bash
bash scripts/tests/nightly_chaos_guard.sh
CHAOS_LOOP=1 INTERVAL_SEC=3600 bash scripts/tests/nightly_chaos_guard.sh
```

Tests: 5000 random pool payouts (ledger drift), HTTP replay/tamper (403/400 + `ipAbuse`), `init-worker.sh` with corrupt `hackme.ini`, critical security pack.
