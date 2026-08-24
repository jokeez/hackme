# HackMe 0.1.0-rc11m — archived download channel

> **Historical archive.** Current channel: [HACKME_RC15.md](../../HACKME_RC15.md) (`0.1.0-rc15`).


**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — wallet treasury fix, canonical on-chain HMC + SUP display.

## Artifacts

| Artifact | Channel | Notes |
|----------|---------|-------|
| Windows installer | **rc11m** | `HackMe-Setup-0.1.0-rc11m.exe` |
| Linux tarball | **rc11m** | `hackme_0.1.0-rc11m_linux.tar.gz` |
| HackMe OS ISO | **rc11l** | Same live-boot ISO as rc11l until rc11m ISO rebuild — [SHA256SUMS-iso.txt](https://hackme.tech/dist/release_0.1.0-rc11l/SHA256SUMS-iso.txt) |

## What changed vs rc11l

- Unified wallet balance model: **on-chain · pool accrual · orders**
- Canonical peer cache includes **HMC + SUP**; desktop fast path uses stale cache
- Dashboard wallet tab: treasury breakdown + `refreshWalletCanonicalBoost()` from hackme.tech
- VPS + desktop node embed **0.1.0-rc11m** (rebuild/deploy required after pull)

## Downloads

- Base: `https://hackme.tech/dist/release_0.1.0-rc11m/`
- ISO (rc11l): `https://hackme.tech/dist/release_0.1.0-rc11l/HackMe-OS-0.1.0-rc11l-amd64.iso`

## Operator

```bash
bash scripts/tests/version_consistency_gate.sh
bash scripts/ops/verify_public_download.sh
bash scripts/tests/verify_hackme_iso.sh dist/release_0.1.0-rc11l/HackMe-OS-0.1.0-rc11l-amd64.iso
NODE_SSH=root@host bash scripts/ops/deploy_hackme_public_stack.sh
```

Historical: [HACKME_RC11L.md](HACKME_RC11L.md) (ISO boot fix) · [HACKME_RC11K_LAUNCH_CANDIDATE.md](HACKME_RC11K_LAUNCH_CANDIDATE.md)
