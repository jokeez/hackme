# HackMe 0.1.0-rc11p — superseded by rc11r

> **Current channel:** [HACKME_RC11R.md](HACKME_RC11R.md) · `0.1.0-rc11r` on [downloads](https://hackme.tech/downloads.html)

**Status:** Superseded — kept for changelog reference.

## Artifacts (historical)

| Artifact | Channel | Notes |
|----------|---------|-------|
| Windows installer | **rc11p** | `HackMe-Setup-0.1.0-rc11p.exe` |
| Linux tarball | **rc11p** | `hackme_0.1.0-rc11p_linux.tar.gz` |
| Fuzz CLI | **rc11p** | `hackme-fuzzing-0.1.0-rc11p-*` |
| HackMe OS ISO | **rc11o** | Pre-rc11q ISO channel |

## What changed vs rc11o

- Coordinator **hashrate reporting**: peak/smooth EMA recovery; submit prefers worker-reported `hashrate_gh_s`
- **Claim rate floor** for high global limits (fixes home GPU stuck at ~0 GH/s on pool)
- Dashboard: local worker row uses **measured_hashrate_gh_s** when coordinator under-reports
- **Metrics** `/api/metrics`: DB timeout + skip heavy reward breakdown on pool followers (fixes multi-minute hangs)
- SQLite WAL: checkpoint guidance after long desktop runs

## Downloads

- Base: `https://hackme.tech/dist/release_0.1.0-rc11p/`
- ISO (rc11o): `https://hackme.tech/dist/release_0.1.0-rc11o/HackMe-OS-0.1.0-rc11o-amd64.iso`

## Operator

```bash
bash scripts/tests/version_consistency_gate.sh
bash scripts/ops/new_miner_journey_gate.sh
bash scripts/ops/run_miner_launch_gate.sh
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_node.sh
```

Historical: [HACKME_RC11N.md](HACKME_RC11N.md) · [HACKME_RC11O.md](HACKME_RC11O.md) if present
