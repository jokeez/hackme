# HackMe 0.1.0-rc12w — wallet Activity + security hardening

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux ZIP + tarball, fuzz CLI, and HackMe OS ISO on a single **rc12w** channel.

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

## SHA256 (rc12w)

```
2063140c9c9d8d25e48dd3b1bb06e436dc493e9d52daa026a3f788a26245d87b  hackme_0.1.0-rc12w_windows.zip
2880d7d17bdf451aff2eef363a474e353a1b3c45ade323aefaacca7648ed45c8  hackme_0.1.0-rc12w_windows_setup.zip
3128b17d73c278f0d5fc8db9ad51ce8540dec9512ee7576e38116f7ca6ae6400  HackMe-Setup-0.1.0-rc12w.exe
ee71c212ecbc8feea0efd95418973664774e3e483ee6025b7fb224bbd6848c4a  hackme_0.1.0-rc12w_linux.tar.gz
33f1a43df66551ce1bbad4bde97ef51333892b5a26bda7ddd24b536ca11321bf  hackme-fuzzing-0.1.0-rc12w-linux-amd64
4359b260c35eb4419a60336bb043379a7d3582337da24e439f3137bd7096d9ed  hackme-fuzzing-0.1.0-rc12w-windows-amd64.exe
f3d7aad31256e63bd437d78c4cb34d04901b40d1655d2a27e9e7fe006a3d4bf0  hackme-fuzzing-build-0.1.0-rc12w-linux-amd64
8314d69cdac66d7781e1dd5acde2286a007ec9de2ca0f19478b5920c2458eaaa  hackme-fuzzing-build-0.1.0-rc12w-windows-amd64.exe
b4632f72008791878121124a483ef4841c3b791562740996f64671f1dc410dd4  HackMe-OS-0.1.0-rc12w-amd64.iso
```

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
