# HackMe 0.1.0-rc11s — current download channel (production baseline)

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux installer, tarball, fuzz CLI, and HackMe OS ISO on a single aligned channel.

## Artifacts

| Artifact | Channel | File |
|----------|---------|------|
| Windows installer | **rc11s** | `HackMe-Setup-0.1.0-rc11s.exe` |
| Linux tarball | **rc11s** | `hackme_0.1.0-rc11s_linux.tar.gz` |
| Fuzz CLI | **rc11s** | `hackme-fuzzing-0.1.0-rc11s-*` |
| HackMe OS ISO | **rc11s** | `HackMe-OS-0.1.0-rc11s-amd64.iso` |

## What changed vs rc11r

### Mining dashboard

- **Canonical economics overlay** — desktop pool followers show **Actual base/orders/total (1h)** from hackme.tech (proxy-free HTTP client).
- **`GET /api/canonical/metrics`** — same-origin fallback for dashboard widgets.

### Fuzz / escrow

- **Settle outbox drain** — stale `bounty_paid`/`closed` rows no longer block coordinator payout queue.
- **Customer smoke ops** — `scripts/ops/run_customer_pool_smoke.sh` (64 pool runs + escrow snapshot).

### Ops

- Daily coordinator queue cleanup systemd timer (`hackme-pool-fuzz-queue-cleanup`).
- Settlement test isolation fix (`settlement_state_test.go`).

## Downloads

- Win/Linux: `https://hackme.tech/dist/release_0.1.0-rc11s/`
- ISO: `https://hackme.tech/dist/release_0.1.0-rc11s/HackMe-OS-0.1.0-rc11s-amd64.iso`
- GitHub: `https://github.com/jokeez/hackme/releases/tag/0.1.0-rc11s`
- SHA256: `SHA256SUMS.txt` + `SHA256SUMS-iso.txt`
- Notices: `https://hackme.tech/assets/miner-notices.json`

## Linux quick start (miners)

```bash
tar -xzf hackme_0.1.0-rc11s_linux.tar.gz
cd linux
bash setup_hackme_miner.sh    # first run
bash start_hackme_miner.sh
```

## Operator rebuild

```bash
bash scripts/ops/release_rc11s_publish.sh
# or:
VERSION=0.1.0-rc11s bash scripts/release/make_release_bundle.sh
VERSION=0.1.0-rc11s bash scripts/release/iso/build_hackme_miner_iso.sh
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_site.sh
```

Historical: [HACKME_RC11R.md](HACKME_RC11R.md) · [archive/rc/HACKME_RC11Q.md](archive/rc/HACKME_RC11Q.md)
