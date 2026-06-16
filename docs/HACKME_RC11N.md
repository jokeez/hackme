# HackMe 0.1.0-rc11n — current download channel (production final)

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux + ISO on one channel; SUP on-chain; public economics API.

## Artifacts

| Artifact | Channel | Notes |
|----------|---------|-------|
| Windows installer | **rc11n** | `HackMe-Setup-0.1.0-rc11n.exe` |
| Linux tarball | **rc11n** | `hackme_0.1.0-rc11n_linux.tar.gz` |
| HackMe OS ISO | **rc11n** | Aligned with Win/Linux — `HackMe-OS-0.1.0-rc11n-amd64.iso` |
| Fuzz CLI | **rc11n** | `hackme-fuzzing-0.1.0-rc11n-*` |

## What changed vs rc11m

- VPS **node HTTP watchdog** (`hackme-node-watchdog.timer`) restarts hung `hackme-node`
- `transfer_demo.sh` uses **canonical nonce** from hackme.tech
- **SUP Phase C** — on-chain mint + coordinator `on_chain_settle`; public `GET /api/sup/economics`
- **ISO channel** bumped from rc11l → rc11n (same live-boot stack as Win/Linux)
- `scripts/tests/payments_e2e_max.sh` — repeatable payments E2E pack

## Downloads

- Base: `https://hackme.tech/dist/release_0.1.0-rc11n/`
- ISO: `https://hackme.tech/dist/release_0.1.0-rc11n/HackMe-OS-0.1.0-rc11n-amd64.iso`
- ISO sums: `https://hackme.tech/dist/release_0.1.0-rc11n/SHA256SUMS-iso.txt`

## Operator

```bash
bash scripts/tests/version_consistency_gate.sh
bash scripts/ops/sup_full_verdict_gate.sh
bash scripts/ops/run_miner_launch_gate.sh
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_public_stack.sh
```

Historical: [HACKME_RC11M.md](HACKME_RC11M.md) · [HACKME_RC11L.md](HACKME_RC11L.md)
