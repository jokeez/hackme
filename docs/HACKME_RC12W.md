# HackMe 0.1.0-rc12w — wallet Activity + security hardening

> **Superseded.** Current download channel is **[0.1.0-rc14](HACKME_RC14.md)** on [downloads](https://hackme.tech/downloads.html).

**Status:** **historical** — rc12w was the previous Win/Linux/fuzz/ISO channel (wallet Activity + hub hardening).

## Artifacts

| Artifact | Channel | File |
|----------|---------|------|
| Windows portable | **rc12w** | `hackme_0.1.0-rc12w_windows_setup.zip` |
| Linux tarball | **rc12w** | `hackme_0.1.0-rc12w_linux.tar.gz` |
| Fuzz CLI | **rc12w** | `hackme-fuzzing-0.1.0-rc12w-*` |
| Fuzz build helper | **rc12w** | `hackme-fuzzing-build-0.1.0-rc12w-*` |
| HackMe OS ISO | **rc12w** | `HackMe-OS-0.1.0-rc12w-amd64.iso` (~834 MB) |
| Windows installer | **rc12w** | `HackMe-Setup-0.1.0-rc12w.exe` (~8.3 MB) |

## What changed vs rc12u/rc12t

### Wallet dashboard

- **Activity tab** — `GET /api/wallet/activity` with counterparty rollup, In/Out filter, CSV export, address labels (dev fee, pool settlement, market/orders, escrow, self).
- **HMC Overview** — single breakdown: on-chain spendable | pool pending | orders escrow; collapsible chain details.
- **SUP Overview** — on-chain | pool pending mint | mining bonus policy; no duplicate HMC banners.
- **Transfer** — clean Send form; pro fields / explorer / tx pool in `<details>`.
- **Earnings** — timeline collapsed; table capped at 21 days.

### Security (hub + node)

- Dual-ledger consistency, payout lock, admin bind hardening.
- Stratum / payment HMAC, mint idempotency, ImportPoHBlock fixes.
- nginx: `/api/wallet/activity` on public wallet allowlist.

### Ops

- Hub deployed `0.1.0-rc12w` (`a13ccab`).
- `scripts/ops/vps_patch_nginx_wallet_public.sh` includes `wallet/activity`.

## Downloads

- Win/Linux/ISO: `https://hackme.tech/dist/release_0.1.0-rc12w/`
- ISO: `https://hackme.tech/dist/release_0.1.0-rc12w/HackMe-OS-0.1.0-rc12w-amd64.iso`
- GitHub: `https://github.com/jokeez/hackme/releases/tag/0.1.0-rc12w`
- SHA256: `SHA256SUMS.txt` + `SHA256SUMS-iso.txt`

## SHA256 (rc12w — hotfix rebuild 2026-07-22, commit e525544)

```
5d076aa0f83e262f4784c929b66e019122949c5cecdc3fe294e6b18722c1b4ea  hackme_0.1.0-rc12w_windows.zip
a84a9ee5174e0e26c42ebcd3757f07ec5256b11cc1bedd63b24f956c95df402f  hackme_0.1.0-rc12w_windows_setup.zip
323666a0fee158b725f1eac1938b9430a6a2076535a9e3de854d9f400565ffcb  HackMe-Setup-0.1.0-rc12w.exe
0e22c3c70444da8ec04b70c2ada973dd1d875cfa674c494b994e62a92a9c9023  hackme_0.1.0-rc12w_linux.tar.gz
d3bf1ae1930679ee2fd21c48381c2dcdc15dc49ff88199fc095fa6a24eba791a  hackme-fuzzing-0.1.0-rc12w-linux-amd64
4649812aa2b04e321c8d3dd385663345eb3b5304b078e28ebd4fc6dfcb82cbaa  hackme-fuzzing-0.1.0-rc12w-windows-amd64.exe
f3ab4cc1a70c5767aa8fa6e2130557fd3c6f8ba382efe23478cd863a06d7a891  hackme-fuzzing-build-0.1.0-rc12w-linux-amd64
45fb03bede5cfd2f901eb7098a8d861036b527b2645fc3070c43a8733abc4ce5  hackme-fuzzing-build-0.1.0-rc12w-windows-amd64.exe
3ee42352749624be2e486c63d123ada943c84d058426e222d415c4fb068ba005  HackMe-OS-0.1.0-rc12w-amd64.iso
```

Hotfix in this rebuild: desktop pool **pending settlement** no longer stuck at 0 HMC (poisoned local canonical cache). Same version tag — **re-download** and verify SHA256.

## Linux quick start (miners)

```bash
tar -xzf hackme_0.1.0-rc12w_linux.tar.gz
cd linux
bash setup_hackme_miner.sh    # first run
bash start_hackme_miner.sh
```

## Operator rebuild

```bash
bash scripts/ops/release_rc12w_publish.sh
# or:
VERSION=0.1.0-rc12w bash scripts/release/make_release_bundle.sh
SKIP_ISO=1 bash scripts/ops/release_rc12w_publish.sh   # skip ISO rebuild when unchanged
NODE_SSH=hackme-vps SYNC_DIST=1 bash scripts/ops/deploy_hackme_site.sh
```

Historical: [HACKME_RC11S.md](HACKME_RC11S.md)
