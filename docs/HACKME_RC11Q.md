# HackMe 0.1.0-rc11q — current download channel

**Status:** **LIVE** on [hackme.tech/downloads.html](https://hackme.tech/downloads.html) — Win/Linux installer, tarball, fuzz CLI, and HackMe OS ISO on a single aligned channel.

## Artifacts

| Artifact | Channel | File |
|----------|---------|------|
| Windows installer | **rc11q** | `HackMe-Setup-0.1.0-rc11q.exe` |
| Linux tarball | **rc11q** | `hackme_0.1.0-rc11q_linux.tar.gz` |
| Fuzz CLI | **rc11q** | `hackme-fuzzing-0.1.0-rc11q-*` |
| HackMe OS ISO | **rc11q** | `HackMe-OS-0.1.0-rc11q-amd64.iso` |

## What changed vs rc11p

- **Settlement API** — `/api/worker/settlement` no longer times out on the public hub; cached coordinator work stats + 8s budget
- **Live update banner** — ecosystem tab reads `miner-notices.json` (no exe rebuild for copy/upgrade nudges)
- **Lite status audit** — `?lite=1` exposes `sandbox_policy` + `admin_auth_enabled` for public security checks
- **Code health** — status + settlement handlers extracted from `main.go` (~800 lines) into focused modules
- **ISO aligned** — same `rc11q` tag as Win/Linux (was rc11o on CDN)

## Downloads

- Win/Linux: `https://hackme.tech/dist/release_0.1.0-rc11q/`
- ISO: `https://hackme.tech/dist/release_0.1.0-rc11q/HackMe-OS-0.1.0-rc11q-amd64.iso`
- Notices feed: `https://hackme.tech/assets/miner-notices.json`

## Operator

```bash
bash scripts/tests/version_consistency_gate.sh
bash scripts/ops/release_rc11q_publish.sh
# or SKIP_ISO=1 for Win/Linux only
NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_node.sh
```

Historical: [HACKME_RC11P.md](HACKME_RC11P.md) · [HACKME_RC11O.md](HACKME_RC11O.md)
