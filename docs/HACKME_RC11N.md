# HackMe 0.1.0-rc11n — current download channel

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — node HTTP watchdog, canonical transfer nonce fix, mirror test isolation.

## Artifacts

| Artifact | Channel | Notes |
|----------|---------|-------|
| Windows installer | **rc11n** | `HackMe-Setup-0.1.0-rc11n.exe` |
| Linux tarball | **rc11n** | `hackme_0.1.0-rc11n_linux.tar.gz` |
| HackMe OS ISO | **rc11l** | Unchanged live-boot ISO — [SHA256SUMS-iso.txt](https://hackme.tech/dist/release_0.1.0-rc11l/SHA256SUMS-iso.txt) |

## What changed vs rc11m

- VPS **node HTTP watchdog** (`hackme-node-watchdog.timer`) restarts hung `hackme-node`
- `transfer_demo.sh` uses **canonical nonce** from hackme.tech when testing local desktop nodes
- `TestEnrichWorkStatsDesktopWorkerFromMirror` isolated from live worker logs
- `scripts/tests/payments_e2e_max.sh` — repeatable payments E2E pack

## Downloads

- Base: `https://hackme.tech/dist/release_0.1.0-rc11n/`
- ISO (rc11l): `https://hackme.tech/dist/release_0.1.0-rc11l/HackMe-OS-0.1.0-rc11l-amd64.iso`

## Operator

```bash
bash scripts/tests/version_consistency_gate.sh
bash scripts/tests/payments_e2e_max.sh
NODE_SSH=root@host bash scripts/ops/deploy_hackme_public_stack.sh
```

Historical: [HACKME_RC11M.md](HACKME_RC11M.md) · [HACKME_RC11L.md](HACKME_RC11L.md)
