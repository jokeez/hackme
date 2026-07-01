# HackMe 0.1.0-rc11r — current download channel

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux installer, tarball, fuzz CLI, and HackMe OS ISO on a single aligned channel.

## Artifacts

| Artifact | Channel | File |
|----------|---------|------|
| Windows installer | **rc11r** | `HackMe-Setup-0.1.0-rc11r.exe` |
| Linux tarball | **rc11r** | `hackme_0.1.0-rc11r_linux.tar.gz` |
| Fuzz CLI | **rc11r** | `hackme-fuzzing-0.1.0-rc11r-*` |
| HackMe OS ISO | **rc11r** | `HackMe-OS-0.1.0-rc11r-amd64.iso` |

## What changed vs rc11q

### Mining (Linux + ISO)

- **Release layout** — Linux tarball ships `scripts/ops/worker_autostart.sh` and `bin/workerpoh-opencl` (fixes `worker_script_missing` / exit 127 on fresh extract).
- **`fix_miner_layout.sh`** — one-command repair for older `linux/` folders missing `scripts/ops/` or `bin/`.
- **Node fallback** — desktop node finds flat or nested worker scripts and worker binaries.

### Fuzz B2B / payouts

- **Pull-mode settle** — coordinator outbox + desktop ack; nginx routes for `/api/fuzz/pool/settle/outbox`.
- **Escrow cleanup** — stale open escrows finalize on campaign terminal status; `cleanup-stale` operator API.
- **Settlement timer** — VPS mining settlement re-enabled (accrual → on-chain sweep).

### Ops / security

- Security full audit 16/16 PASS on prod.
- `gofmt` + CI hygiene on poolfuzz relay.

## Downloads

- Win/Linux: `https://hackme.tech/dist/release_0.1.0-rc11r/`
- ISO: `https://hackme.tech/dist/release_0.1.0-rc11r/HackMe-OS-0.1.0-rc11r-amd64.iso`
- SHA256: `SHA256SUMS.txt` + `SHA256SUMS-iso.txt`
- Notices: `https://hackme.tech/assets/miner-notices.json`

## Linux quick start (miners)

```bash
tar -xzf hackme_0.1.0-rc11r_linux.tar.gz
cd linux
bash setup_hackme_miner.sh    # first run
bash start_hackme_miner.sh
# stuck on old folder?
bash fix_miner_layout.sh && bash start_hackme_miner.sh
```

## Operator rebuild

```bash
VERSION=0.1.0-rc11r CGO_ENABLED=1 bash scripts/release/make_release_bundle.sh
VERSION=0.1.0-rc11r bash scripts/release/iso/build_hackme_miner_iso.sh
bash scripts/tests/site_release_consistency_gate.sh
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_site.sh
```

Historical: [HACKME_RC11Q.md](HACKME_RC11Q.md) · [HACKME_RC11P.md](HACKME_RC11P.md)
